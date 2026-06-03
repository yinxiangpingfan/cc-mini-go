package agent

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/yinxiangpingfan/cc-mini-go/agent/core"
	"github.com/yinxiangpingfan/cc-mini-go/agent_tools"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/config"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
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
}

// microcompactArmed 时间闸：仅当距上次活动 >= GapThreshold（prompt cache 大概率已失效）
// 才允许触发 microcompact。首次调用（无上次活动）不触发——本就没有旧结果可压。
func (a *ChatCompletionAgent) microcompactArmed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lastActivityAt.IsZero() {
		return false
	}
	return time.Since(a.lastActivityAt) >= agent_tools.GapThreshold
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
	tools := make(map[string]agent_tools.ToolFunc)
	clientTool := a.ToolInit(&tools)
	//把 skill 目录拼进 system prompt（轻量发现层）
	system = a.withSkillCatalog(system)
	//把跨会话记忆拼进 system prompt（读取层）
	system = a.withMemory(system)
	//上下文压缩状态（跨轮）
	compactState := agent_tools.NewCompactState()
	//会话转录：整段对话持续以 jsonl 落盘，与压缩解耦
	transcript := agent_tools.NewSessionTranscript()
	//任意返回路径都把最后的消息补写入转录
	defer func() { _ = transcript.Flush(allMsg) }()
	//记录本次对话结束时刻，供下次 microcompact 时间闸判定
	defer a.markActivity()
	//会话级 hook：整个会话仅首条消息触发一次（sessionOnce 守护）
	a.fireSessionStart()

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
			allMsg = agent_tools.MicroCompact(allMsg)
		}
		// 整体过大才做完整压缩（按体积，每轮判断）
		if agent_tools.EstimateContextSize(allMsg) > agent_tools.ContextLimit {
			allMsg = agent_tools.CompactHistory(ctx, a.call, a.cf.Model, allMsg, compactState, "")
		}

		// 本轮计数 +1（计划已多少轮未更新）
		agent_tools.ToDoList.MU.Lock()
		if len(agent_tools.ToDoList.Items) > 0 {
			agent_tools.ToDoList.RoundsSinceUpdate++
		}
		agent_tools.ToDoList.MU.Unlock()

		// 检查是否需要刷新计划
		agent_tools.ToDoList.MU.RLock()
		needReminder := agent_tools.ToDoList.RoundsSinceUpdate >= 3
		agent_tools.ToDoList.MU.RUnlock()
		if needReminder {
			reminder := a.call.Cm.NewUserMessage("<reminder>Refresh your plan before continuing.</reminder>")
			allMsg = append(allMsg, reminder)
		}

		// LLM 调用：自动重试网络错误与限流/5xx，不可重试错误直接返回
		res, err := a.callWithRetry(ctx, allMsg, system, clientTool)
		if err != nil {
			return allMsg, err
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
			//追加工具请求信息
			allMsg = append(allMsg, *a.call.Cm.NewToolsCall(res.Choices[0].Message.Content, res.Choices[0].Message.ToolCalls))
			//助手在调用工具的同时可能带文本，一并广播
			if s, ok := res.Choices[0].Message.Content.(string); ok && s != "" {
				a.emit(AgentEvent{Type: EventContent, Text: s})
			}
			//检测本轮是否请求手动压缩
			manualCompact, compactFocus := agent_tools.DetectManualCompact(res.Choices[0].Message.ToolCalls)
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
				allMsg = agent_tools.CompactHistory(ctx, a.call, a.cf.Model, allMsg, compactState, compactFocus)
			}
		} else {
			//没有工具调用，返回结果
			if s, ok := res.Choices[0].Message.Content.(string); ok && s != "" {
				a.emit(AgentEvent{Type: EventContent, Text: s})
				allMsg = append(allMsg, *a.call.Cm.NewAssistantMessage(s))
			}
			return allMsg, nil
		}
	}
	//超过最大轮次仍未收敛，返回错误兜底
	return allMsg, errors.ErrAgentMaxTurns
}
