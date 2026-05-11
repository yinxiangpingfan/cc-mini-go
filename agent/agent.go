package agent

import (
	"encoding/json"
	"sync"

	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/config"
	tool "github.com/yinxiangpingfan/cc-mini-go/tools"
)

type ChatCompletionAgent struct {
	cf   *config.Config
	call *client.Call
}

func NewChatCompletionAgent(cf *config.Config, call *client.Call) *ChatCompletionAgent {
	return &ChatCompletionAgent{
		cf:   cf,
		call: call,
	}
}

// 非流式对话请求
func (a *ChatCompletionAgent) Agent(messages []client.Message, system string) ([]any, error) {
	//定义信息
	allMsg := make([]any, 0, len(messages))
	for _, m := range messages {
		allMsg = append(allMsg, m)
	}
	//存储工具信息与调用函数
	tools := make(map[string]func(input map[string]any) string)
	timeNowTool := tool.NewTimeNowTool()
	tools[timeNowTool.Name] = timeNowTool.Func
	//开始请求LLM
	for {
		res, resp, err := a.call.NewCallRequest(a.cf.Model, allMsg, false, system, []client.Tool{
			timeNowTool.TimeNowInfoForLLm(),
		}, nil)
		if resp.StatusCode != 200 {
			//TODO:处理错误
			return allMsg, err
		}
		//处理LLM返回的信息
		if res.Choices[0].Message.Refusal != "" {
			//大模型拒绝回答
			allMsg = append(allMsg, *a.call.Cm.NewAssistantMessage(res.Choices[0].Message.Refusal))
			return allMsg, nil
		}
		if res.Choices[0].Message.Content != "" {
			allMsg = append(allMsg, *a.call.Cm.NewAssistantMessage(res.Choices[0].Message.Content))
		}
		//处理工具调用
		if len(res.Choices[0].Message.ToolCalls) > 0 {
			//追加工具请求信息
			allMsg = append(allMsg, *a.call.Cm.NewToolsCall(res.Choices[0].Message.ToolCalls))
			var wg sync.WaitGroup
			var mu sync.Mutex
			for _, v := range res.Choices[0].Message.ToolCalls {
				if f, exists := tools[v.Function.Name]; exists {
					wg.Add(1)
					go func() {
						defer wg.Done()
						var args map[string]any
						json.Unmarshal([]byte(v.Function.Arguments), &args)
						res := f(args)
						mu.Lock()
						//追加工具返回信息
						allMsg = append(allMsg, *a.call.Cm.NewToolsMessage(v.Id, res))
						mu.Unlock()
					}()
				}
			}
			wg.Wait()
		} else {
			//没有工具调用，返回结果
			return allMsg, nil
		}
	}
}
