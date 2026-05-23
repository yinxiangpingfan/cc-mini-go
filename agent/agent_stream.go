package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	tool "github.com/yinxiangpingfan/cc-mini-go/agent_tools"
	"github.com/yinxiangpingfan/cc-mini-go/client"
)

func (a *ChatCompletionAgent) StreamAgent(messages []client.Message, system string) ([]any, error) {
	//定义信息
	allMsg := make([]any, 0, len(messages))
	for _, m := range messages {
		allMsg = append(allMsg, m)
	}
	//存储工具信息与调用函数
	tools := make(map[string]func(input map[string]any) string)
	timeNowTool := tool.NewTimeNowTool()
	tools[timeNowTool.Name] = timeNowTool.Func
	readFileTool := tool.NewReadFile()
	tools[readFileTool.Name] = readFileTool.Func

	//定义回调函数
	activeToolCalls := make(map[int]*client.StreamToolCall) // 当前存在的 ToolCalls
	var contentBuilder strings.Builder                      // 累积本轮 assistant 的 content
	cur := false
	cur1 := false
	onMessage := func(sr client.StreamResponse) {
		if len(sr.Choices) == 0 {
			return
		}
		if sr.Choices[0].Delta.ReasoningContent != "" {
			if cur == false {
				fmt.Println("思考")
				cur = true
			}
			fmt.Print(sr.Choices[0].Delta.ReasoningContent)
		}
		if sr.Choices[0].Delta.Content != "" {
			if cur1 == false {
				fmt.Println("\n对话")
				cur1 = true
			}
			fmt.Print(sr.Choices[0].Delta.Content)
			contentBuilder.WriteString(sr.Choices[0].Delta.Content)
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
	//开始请求LLM
	for {
		//每轮开始前重置累积器
		contentBuilder.Reset()
		activeToolCalls = make(map[int]*client.StreamToolCall)

		_, _, err := a.call.NewCallRequest(a.cf.Model, allMsg, true, system, []client.Tool{
			timeNowTool.TimeNowInfoForLLm(),
			readFileTool.ReadFileInfoForLLm(),
		}, onMessage)
		if err != nil && !errors.Is(err, io.EOF) {
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

		// 并发执行工具
		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, v := range activeToolCalls {
			if v == nil || v.Function == nil || v.Function.Name == nil {
				continue
			}
			if f, exists := tools[*v.Function.Name]; exists {
				wg.Add(1)
				go func(v *client.StreamToolCall) {
					defer wg.Done()
					var args map[string]any
					if v.Function.Arguments != nil {
						json.Unmarshal([]byte(*v.Function.Arguments), &args)
					}
					res := f(args)
					mu.Lock()
					//追加工具返回信息
					allMsg = append(allMsg, *a.call.Cm.NewToolsMessage(*v.Id, res))
					mu.Unlock()
				}(v)
			}
		}
		wg.Wait()
	}
}
