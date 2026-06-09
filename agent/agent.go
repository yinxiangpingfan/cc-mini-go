package agent

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/yinxiangpingfan/cc-mini-go/agent/core"
	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/ctxmgmt"
	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/config"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

type ChatCompletionAgent struct {
	cf     *config.Config
	call   *client.Call
	events chan<- AgentEvent // 可选：向外广播进度事件（如重试），nil 表示不广播

	perms   *core.PermissionEngine // 可选：工具执行前的权限闸；nil 表示不启用，跳过检查
	approve ApprovalFunc           // 可选：ask 判定时的人机确认回调；nil 时 ask 按拒绝处理
	hooks   *core.HookRunner       // 可选：工具前后的 hook 扩展点；nil 表示不启用，所有时机放行

	builtinHooks bool      // 是否注册内置 hook（WithBuiltinHooks 开启），在构造末尾生效
	sessionOnce  sync.Once // 保证 SessionStart 整个会话只触发一次（Agent/StreamAgent 每条消息都重入）

	permMu     sync.Mutex // 保护 denyStreak（工具在并发 goroutine 中各自判定）
	denyStreak int        // 连续被拒次数，达阈值发 EventPermissionHint 后清零

	mu             sync.Mutex // 保护 lastActivityAt（跨多次 Agent/StreamAgent 调用）
	lastActivityAt time.Time  // 上次对话结束时刻，作为 microcompact 时间闸的基准

	lastPromptTokens int // 上次 API 报告的 prompt_tokens（token 预算的权威基线，0=本轮还没调过/已因压缩失效）
}

// microcompactArmed 时间闸：仅当距上次活动 >= GapThreshold（prompt cache 大概率已失效）
// 才允许触发 microcompact。首次调用（无上次活动）不触发——本就没有旧结果可压。
func (a *ChatCompletionAgent) microcompactArmed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lastActivityAt.IsZero() {
		return false
	}
	return time.Since(a.lastActivityAt) >= ctxmgmt.GapThreshold
}

// markActivity 记录本次对话结束时刻，作为下次时间闸的基准。
func (a *ChatCompletionAgent) markActivity() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastActivityAt = time.Now()
}

func NewChatCompletionAgent(cf *config.Config, call *client.Call, opts ...AgentOption) *ChatCompletionAgent {
	a := &ChatCompletionAgent{
		cf:   cf,
		call: call,
	}
	for _, opt := range opts {
		opt(a)
	}
	// 选项应用完毕后再注册内置 hook：此时 hooks/events 等都已就位，且能叠加到自定义 runner 上。
	if a.builtinHooks {
		a.registerBuiltinHooks()
	}
	return a
}

// 非流式对话请求。messages 为完整历史（异构 []any），进出同型以便调用方闭环回传。
// ctx 取消会中断进行中的 LLM 请求并尽快退出循环。
func (a *ChatCompletionAgent) Agent(ctx context.Context, messages []any, system string) ([]any, error) {
	//定义信息：拷贝一份，避免就地改写调用方持有的历史切片
	allMsg := append([]any(nil), messages...)
	//存储工具信息与调用函数
	tools := make(map[string]shared.ToolFunc)
	clientTool := a.ToolInit(&tools)
	//把 system prompt 组装成分段流水线：core + skills + memory + CLAUDE.md + 动态环境（s10）
	system = a.buildSystemPrompt(system)
	//上下文压缩状态（跨轮）
	compactState := ctxmgmt.NewCompactState()
	//会话转录：整段对话持续以 jsonl 落盘，与压缩解耦
	transcript := ctxmgmt.NewSessionTranscript()
	//任意返回路径都把最后的消息补写入转录
	defer func() { _ = transcript.Flush(allMsg) }()
	//记录本次对话结束时刻，供下次 microcompact 时间闸判定
	defer a.markActivity()
	//会话级 hook：整个会话仅首条消息触发一次（sessionOnce 守护）
	a.fireSessionStart()
	//错误恢复预算（续写 / 压缩重试），跨轮累计（s11）
	var rec recoveryState
	//上次发给 API 的消息条数：其后新增的消息按「尾巴」估算 token（配合 lastPromptTokens 权威基线）
	sentCount := 0

	//开始请求LLM（带最大轮次保护，防止工具调用无限循环）
	for turn := 0; turn < agentMaxTurns; turn++ {
		// 每轮开始检查取消，尽快退出
		if err := ctx.Err(); err != nil {
			return allMsg, err
		}
		// 持续把本轮之前新增的消息落盘（压缩前先写，保住完整历史）
		_ = transcript.Flush(allMsg)
		// microcompact 时间闸：仅在「跨轮空闲超阈值」时于首轮压一次旧工具结果；
		// 活跃会话内（轮间秒级）不压——对齐源码，避免读 6 个文件就清掉第 1 个
		if turn == 0 && a.microcompactArmed() {
			allMsg = ctxmgmt.MicroCompact(allMsg)
		}
		// 整体过大才做完整压缩（按 token 估算 vs 由窗口推导的自动压缩阈值，每轮判断）
		if a.estimateContextTokens(allMsg, sentCount) > ctxmgmt.AutoCompactThreshold(a.cf.ContextWindow()) {
			allMsg = ctxmgmt.CompactHistory(ctx, a.call, a.cf.Model, allMsg, compactState, "")
			a.lastPromptTokens = 0 // 历史被重写，旧基线失效，待下次调用重新校准
		}

		// 本轮计数 +1（计划已多少轮未更新）
		shared.ToDoList.MU.Lock()
		if len(shared.ToDoList.Items) > 0 {
			shared.ToDoList.RoundsSinceUpdate++
		}
		shared.ToDoList.MU.Unlock()

		// 检查是否需要刷新计划
		shared.ToDoList.MU.RLock()
		needReminder := shared.ToDoList.RoundsSinceUpdate >= 3
		shared.ToDoList.MU.RUnlock()
		if needReminder {
			reminder := a.call.Cm.NewUserMessage("<reminder>Refresh your plan before continuing.</reminder>")
			allMsg = append(allMsg, reminder)
		}

		// LLM 调用：自动重试网络错误与限流/5xx，不可重试错误直接返回
		sentCount = len(allMsg) // 记录本次发送边界，供下轮估算「新增尾巴」
		res, err := a.callWithRetry(ctx, allMsg, system, clientTool)
		if err != nil {
			// 上下文超长：压缩历史后重试（退避重试无用），限额内 continue
			if k, why := chooseRecovery("", err); k == recoveryCompact && rec.compactAttempts < maxCompactRecovery {
				rec.compactAttempts++
				a.noteRecovery("🗜 " + why + "，压缩后重试")
				_ = transcript.Flush(allMsg)
				allMsg = ctxmgmt.CompactHistory(ctx, a.call, a.cf.Model, allMsg, compactState, "")
				a.lastPromptTokens = 0
				continue
			}
			return allMsg, err
		}
		// 用 API 报告的 prompt_tokens 校准权威基线（含 system/工具 schema/全历史）
		if res.Usage.PromptTokens > 0 {
			a.lastPromptTokens = res.Usage.PromptTokens
		}
		if len(res.Choices) == 0 {
			return allMsg, nil
		}
		//处理LLM返回的信息
		if res.Choices[0].Message.Refusal != "" {
			//大模型拒绝回答
			a.emit(AgentEvent{Type: EventContent, Text: res.Choices[0].Message.Refusal})
			allMsg = append(allMsg, *a.call.Cm.NewAssistantMessage(res.Choices[0].Message.Refusal))
			return allMsg, nil
		}
		//处理工具调用
		if len(res.Choices[0].Message.ToolCalls) > 0 {
			rec.continueAttempts = 0 // 有工具调用=有进展，续写预算清零
			//追加工具请求信息
			allMsg = append(allMsg, *a.call.Cm.NewToolsCall(res.Choices[0].Message.Content, res.Choices[0].Message.ToolCalls))
			//助手在调用工具的同时可能带文本，一并广播
			if s, ok := res.Choices[0].Message.Content.(string); ok && s != "" {
				a.emit(AgentEvent{Type: EventContent, Text: s})
			}
			//检测本轮是否请求手动压缩
			manualCompact, compactFocus := ctxmgmt.DetectManualCompact(res.Choices[0].Message.ToolCalls)
			var wg sync.WaitGroup
			var mu sync.Mutex
			var imageURIs []string    // 图片工具结果拆出的 data URI，待工具结果全部就位后再追加
			var injectedMsgs []string // hook（exit 2）注入的补充消息，同样待工具结果全部就位后再追加
			for _, v := range res.Choices[0].Message.ToolCalls {
				if f, exists := tools[v.Function.Name]; exists {
					wg.Add(1)
					go func() {
						defer wg.Done()
						a.emit(AgentEvent{Type: EventToolStart, ToolID: v.Id, ToolName: v.Function.Name, ToolArgs: v.Function.Arguments})
						var args map[string]any
						json.Unmarshal([]byte(v.Function.Arguments), &args)
						//PreToolUse hook → 权限闸 → 执行 → PostToolUse hook，被拦截仍回传配对的 tool 结果
						content, imageURI, isImage, inject := a.execToolWithHooks(ctx, v.Id, v.Function.Name, v.Function.Arguments, args, f)
						a.emit(AgentEvent{Type: EventToolResult, ToolID: v.Id, ToolName: v.Function.Name, Text: content})
						mu.Lock()
						//追加工具返回信息
						allMsg = append(allMsg, *a.call.Cm.NewToolsMessage(v.Id, content))
						if isImage {
							imageURIs = append(imageURIs, imageURI)
						}
						injectedMsgs = append(injectedMsgs, inject...)
						mu.Unlock()
					}()
				}
			}
			wg.Wait()
			//图片作为独立的多模态 user 消息接在工具结果之后（保证 tool_call 配对先完整）
			for _, uri := range imageURIs {
				allMsg = append(allMsg, *a.call.Cm.NewImageMessage(uri))
			}
			//hook 注入的补充消息接在所有工具结果之后，避免插在 tool_call 与其 result 之间破坏配对
			for _, msg := range injectedMsgs {
				allMsg = append(allMsg, *a.call.Cm.NewUserMessage(msg))
			}
			//手动压缩与自动压缩复用同一条机制（压缩前先把本轮消息落盘）
			if manualCompact {
				_ = transcript.Flush(allMsg)
				allMsg = ctxmgmt.CompactHistory(ctx, a.call, a.cf.Model, allMsg, compactState, compactFocus)
			}
		} else {
			//没有工具调用，本轮是最终文本
			s, _ := res.Choices[0].Message.Content.(string)
			if s != "" {
				a.emit(AgentEvent{Type: EventContent, Text: s})
				allMsg = append(allMsg, *a.call.Cm.NewAssistantMessage(s))
			}
			// 输出被截断（finish_reason==length）→ 续写：保留半截，追加续写提示再来一轮
			if k, _ := chooseRecovery(res.Choices[0].FinishReason, nil); k == recoveryContinue && rec.continueAttempts < maxContinueAttempts {
				rec.continueAttempts++
				a.noteRecovery("↻ 输出被截断，续写中")
				allMsg = append(allMsg, *a.call.Cm.NewUserMessage(prompt.ContinuationPrompt))
				continue
			}
			return allMsg, nil
		}
	}
	//超过最大轮次仍未收敛，返回错误兜底
	return allMsg, errors.ErrAgentMaxTurns
}
