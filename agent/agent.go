package agent

import (
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/config"
	"github.com/yinxiangpingfan/cc-mini-go/tools"
)

type ChatCompletionAgent struct {
	cf    *config.Config
	call  *client.Call
	tools *tools.Tools
}

func NewChatCompletionAgent(cf *config.Config, call *client.Call, tools *tools.Tools) *ChatCompletionAgent {
	return &ChatCompletionAgent{
		cf:    cf,
		call:  call,
		tools: tools,
	}
}

// 非流式对话请求
func (a *ChatCompletionAgent) Agent(messages []client.Message, system string) (string, error) {
	tools := make([]client.Tool, 0)
	for {
		res, resp, err := a.call.NewCallRequest(a.cf.Model, messages, false, system, tools, nil)
		if resp.StatusCode != 200 {
			//TODO:处理错误值
			return "", err
		}
		//判断是否有工具调用
		if len(res.Choices[0].Message.ToolCalls) > 0 {
			res := a.toolsUse(res.Choices[0].Message.ToolCalls)
		}
	}
	return "", nil
}
