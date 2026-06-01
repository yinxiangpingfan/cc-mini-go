package test

import (
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/agent"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/config"
	"github.com/yinxiangpingfan/cc-mini-go/log"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

func TestToolStream(t *testing.T) {
	cf, err := config.GetConfig()
	if err != nil {
		t.Error(err)
	}
	cl, err := client.Init(cf.ApiUrl, cf.ApiKey)
	if err != nil {
		t.Error(err)
	}
	log := log.InitLogger()
	cm := client.NewChatCompletionMessage()
	call := client.NewCall(cl, cm, log)
	a := agent.NewChatCompletionAgent(&cf, call)
	res, err := a.StreamAgent([]client.Message{
		*cm.NewUserMessage("查看/Users/easyimpr/Desktop/cc-mini-go/test/data目录下的文件列表"),
	}, prompt.SystemPrompt)
	if err != nil {
		t.Error(err)
	}
	for _, choice := range res {
		t.Log(choice)
	}
}
