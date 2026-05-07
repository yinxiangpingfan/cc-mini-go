package agent

import (
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/config"
)

type ChatCompletionAgent struct {
	cm   *client.ChatCompletionMessage
	cl   *client.ChatCompletionClient
	call *client.Call
}

func NewChatCompletionAgent(cm *client.ChatCompletionMessage, cl *client.ChatCompletionClient, call *client.Call, config *config.Config) *ChatCompletionAgent {
	return &ChatCompletionAgent{
		cm:   cm,
		cl:   cl,
		call: call,
	}
}

func (a *ChatCompletionAgent) Chat() (string, error) {
	return "", nil // TODO: implement me
}
