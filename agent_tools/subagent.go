package agent_tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

const subAgentMaxTurns = 30

// SubAgentRunner 持有运行一个子 agent 循环所需的依赖。
type SubAgentRunner struct {
	call   *client.Call
	model  string
	system string
}

// run 以全新的消息列表运行一个子 agent，只返回最终的文本摘要。
// 调用结束后子 agent 的上下文被丢弃 —— 父 agent 的上下文保持干净。
func (r *SubAgentRunner) run(ctx context.Context, taskPrompt string) string {
	// 全新上下文：子 agent 不共享父 agent 的对话历史
	subMessages := []any{*r.call.Cm.NewUserMessage(taskPrompt)}

	// 子 agent 工具集：除 "task" 外的所有基础工具（防止无限递归）
	childHandlers := make(map[string]ToolFunc)
	childTools := buildChildTools(&childHandlers)

	var lastText string

	for turn := 0; turn < subAgentMaxTurns; turn++ {
		// 父 ctx 取消则尽快停止子 agent
		if ctx.Err() != nil {
			return jsonErr(ctx.Err().Error())
		}
		res, resp, err := r.call.NewCallRequestCtx(ctx, r.model, subMessages, false, r.system, childTools, nil)
		// 状态码优先：非 200 时 err 携带的是错误响应体，先报状态码再附上正文
		if resp != nil && resp.StatusCode != 200 {
			msg := fmt.Sprintf(errors.ErrHTTPStatusCode, resp.StatusCode)
			if err != nil {
				msg = fmt.Sprintf("%s: %v", msg, err)
			}
			return jsonErr(msg)
		}
		if err != nil {
			return jsonErr(fmt.Errorf("%w: %w", errors.ErrSubAgentRequest, err).Error())
		}
		if len(res.Choices) == 0 {
			break
		}
		choice := res.Choices[0]

		// 没有工具调用 → agent 已完成；记录最终文本
		if len(choice.Message.ToolCalls) == 0 {
			if s, ok := choice.Message.Content.(string); ok && s != "" {
				lastText = s
			}
			break
		}

		// 追加 assistant 的工具调用消息
		subMessages = append(subMessages, *r.call.Cm.NewToolsCall(choice.Message.Content, choice.Message.ToolCalls))

		// 并行执行工具（与父 agent 一致）
		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, tc := range choice.Message.ToolCalls {
			tc := tc
			if f, ok := childHandlers[tc.Function.Name]; ok {
				wg.Add(1)
				go func() {
					defer wg.Done()
					var args map[string]any
					json.Unmarshal([]byte(tc.Function.Arguments), &args)
					result := f(ctx, args)
					mu.Lock()
					subMessages = append(subMessages, *r.call.Cm.NewToolsMessage(tc.Id, result))
					mu.Unlock()
				}()
			}
		}
		wg.Wait()
	}

	if lastText == "" {
		return "(no summary)"
	}
	return lastText
}

// buildChildTools 初始化子 agent 可用的只读 + 写入工具集。
// 故意排除 "task" 工具以防止递归派生子 agent。
func buildChildTools(handlers *map[string]ToolFunc) []client.Tool {
	timeNow := NewTimeNowTool()
	(*handlers)[timeNow.Name] = timeNow.Func

	readFile := NewReadFile()
	(*handlers)[readFile.Name] = readFile.Func

	writeFile := NewWriteFileTool()
	(*handlers)[writeFile.Name] = writeFile.Func

	bash := NewBashTool()
	(*handlers)[bash.Name] = bash.Func

	todoList := NewTodoListTool()
	(*handlers)[todoList.Name] = todoList.Func

	return []client.Tool{
		timeNow.TimeNowInfoForLLm(),
		readFile.ReadFileInfoForLLm(),
		writeFile.WriteFileInfoForLLm(),
		bash.BashToolForLLM(),
		todoList.TodoListInfoLLm(),
	}
}

// NewSubAgentTools 返回一个 Tools，将 "task" 工具派发到一个全新的子 agent 循环。
func NewSubAgentTools(call *client.Call, model string) *Tools {
	runner := &SubAgentRunner{
		call:   call,
		model:  model,
		system: prompt.SubAgentSystemPrompt,
	}
	return &Tools{
		Name: "task",
		Func: func(ctx context.Context, args map[string]any) string {
			// 校验必填参数 prompt
			taskPrompt, ok := args["prompt"].(string)
			if !ok || taskPrompt == "" {
				return jsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "prompt"))
			}
			return runner.run(ctx, taskPrompt)
		},
	}
}

func (t *Tools) SubAgentInfoForLLM() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "task",
			Description: prompt.TaskPrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"prompt": client.ParameterProperty{
						Type:        "string",
						Description: "The task prompt to run in the subagent. Describe what you want the subagent to research or execute.",
					},
				},
				Required: []string{"prompt"},
			},
		},
	}
}
