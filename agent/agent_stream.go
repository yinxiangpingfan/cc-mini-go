package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
)

// StreamAgent 流式对话请求。messages 为完整历史（异构 []any），进出同型以便调用方闭环回传。
// ctx 取消会中断进行中的流式 LLM 请求并尽快退出循环。
func (a *ChatCompletionAgent) StreamAgent(ctx context.Context, messages []any, system string) ([]any, error) {
	//定义信息：拷贝一份，避免就地改写调用方持有的历史切片
	allMsg := append([]any(nil), messages...)
	//存储工具信息与调用函数
	tools := make(map[string]agent_tools.ToolFunc)
	clientTool := a.ToolInit(&tools)
	//把 skill 目录拼进 system prompt（轻量发现层）
	system = a.withSkillCatalog(system)
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

	//定义回调函数
	activeToolCalls := make(map[int]*client.StreamToolCall) // 当前存在的 ToolCalls
	var contentBuilder strings.Builder                      // 累积本轮 assistant 的 content
	onMessage := func(sr client.StreamResponse) {
		if len(sr.Choices) == 0 {
			return
		}
		if rc := sr.Choices[0].Delta.ReasoningContent; rc != "" {
			// 接了事件 channel 走 TUI，否则保持原有 stdout 打印
			if a.events != nil {
				a.emit(AgentEvent{Type: EventReasoning, Text: rc})
			} else {
				fmt.Print(rc)
			}
		}
		if c := sr.Choices[0].Delta.Content; c != "" {
			contentBuilder.WriteString(c) // 累积始终进行，最终文本权威来源
			if a.events != nil {
				a.emit(AgentEvent{Type: EventContent, Text: c})
			} else {
				fmt.Print(c)
			}
		}
		if len(sr.Choices[0].Delta.ToolCalls) > 0 {
			for _, tc := range sr.Choices[0].Delta.ToolCalls {
				idx := tc.Index
				if _, exists := activeToolCalls[idx]; !exists {
					// 初始化当前 Index 的本地缓存对象
					activeToolCalls[idx] = &client.StreamToolCall{}
				}
				// 获取当前这个 Index 对应的本地缓存对象
				activeCall := activeToolCalls[idx]

				// 组装 ID (仅首次出现时 tcChunk.ID 有值)
				if tc.Id != nil && *tc.Id != "" {
					activeCall.Id = tc.Id
					// 初始化 Function
					if activeCall.Function == nil {
						activeCall.Function = &client.StreamFunction{}
					}
				}

				// 组装 Function Name (仅首次出现时有值)
				if tc.Function != nil && tc.Function.Name != nil {
					activeCall.Function.Name = tc.Function.Name
				}

				// 持续拼凑 Arguments
				if tc.Function != nil && tc.Function.Arguments != nil {
					if activeCall.Function == nil {
						activeCall.Function = &client.StreamFunction{}
					}
					if activeCall.Function.Arguments == nil {
						activeCall.Function.Arguments = tc.Function.Arguments
					} else {
						*activeCall.Function.Arguments += *tc.Function.Arguments
					}
				}
			}
		}
	}
	// reset 清空本轮累积器，供每轮开始与流式重试前复用
	reset := func() {
		contentBuilder.Reset()
		activeToolCalls = make(map[int]*client.StreamToolCall)
	}
	//开始请求LLM（带最大轮次保护，防止工具调用无限循环）
	for turn := 0; turn < agentMaxTurns; turn++ {
		// 每轮开始检查取消，尽快退出
		if err := ctx.Err(); err != nil {
			return allMsg, err
		}
		//每轮开始前重置累积器
		reset()

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

		// 流式 LLM 调用：自动重试限流/5xx 与未建连的网络错误
		if err := a.streamWithRetry(ctx, allMsg, system, clientTool, reset, onMessage); err != nil {
			return allMsg, err
		}
		// 构造本轮工具调用列表
		toolCalls := make([]client.ToolCall, 0, len(activeToolCalls))
		for _, v := range activeToolCalls {
			if v == nil || v.Id == nil || v.Function == nil {
				continue
			}
			name, args := "", ""
			if v.Function.Name != nil {
				name = *v.Function.Name
			}
			if v.Function.Arguments != nil {
				args = *v.Function.Arguments
			}
			toolCalls = append(toolCalls, client.ToolCall{
				Id:   *v.Id,
				Type: "function",
				Function: client.FunctionCall{
					Name:      name,
					Arguments: args,
				},
			})
		}
		// 合并本轮 assistant 消息：content + tool_calls 同一条
		var content any
		if contentBuilder.Len() > 0 {
			content = contentBuilder.String()
		}
		assistantMsg := client.ResponseMessage{
			Role:      "assistant",
			Content:   content,
			ToolCalls: toolCalls,
		}
		allMsg = append(allMsg, assistantMsg)

		// 没有工具调用，本轮是最终回复，直接返回
		if len(toolCalls) == 0 {
			return allMsg, nil
		}

		//检测本轮是否请求手动压缩
		manualCompact, compactFocus := agent_tools.DetectManualCompact(toolCalls)

		// 并发执行工具
		var wg sync.WaitGroup
		var mu sync.Mutex
		var imageURIs []string    // 图片工具结果拆出的 data URI，待工具结果全部就位后再追加
		var injectedMsgs []string // hook（exit 2）注入的补充消息，同样待工具结果全部就位后再追加
		for _, v := range activeToolCalls {
			if v == nil || v.Function == nil || v.Function.Name == nil {
				continue
			}
			if f, exists := tools[*v.Function.Name]; exists {
				wg.Add(1)
				go func(v *client.StreamToolCall) {
					defer wg.Done()
					name := *v.Function.Name
					rawArgs := ""
					if v.Function.Arguments != nil {
						rawArgs = *v.Function.Arguments
					}
					a.emit(AgentEvent{Type: EventToolStart, ToolID: *v.Id, ToolName: name, ToolArgs: rawArgs})
					var args map[string]any
					if rawArgs != "" {
						json.Unmarshal([]byte(rawArgs), &args)
					}
					//PreToolUse hook → 权限闸 → 执行 → PostToolUse hook，被拦截仍回传配对的 tool 结果
					content, imageURI, isImage, inject := a.execToolWithHooks(ctx, *v.Id, name, rawArgs, args, f)
					a.emit(AgentEvent{Type: EventToolResult, ToolID: *v.Id, ToolName: name, Text: content})
					mu.Lock()
					//追加工具返回信息
					allMsg = append(allMsg, *a.call.Cm.NewToolsMessage(*v.Id, content))
					if isImage {
						imageURIs = append(imageURIs, imageURI)
					}
					injectedMsgs = append(injectedMsgs, inject...)
					mu.Unlock()
				}(v)
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
	}
	//超过最大轮次仍未收敛，返回错误兜底
	return allMsg, errors.ErrAgentMaxTurns
}
