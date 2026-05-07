package agent

import "github.com/yinxiangpingfan/cc-mini-go/client"

type ChatCompletionAgent struct {
	cm   *client.ChatCompletionMessage
	cl   *client.ChatCompletionClient
	call *client.Call
}

func NewChatCompletionAgent(cm *client.ChatCompletionMessage, cl *client.ChatCompletionClient, call *client.Call) *ChatCompletionAgent {
	return &ChatCompletionAgent{
		cm:   cm,
		cl:   cl,
		call: call,
	}
}
