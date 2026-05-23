package test

import (
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/agent"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/config"
	"github.com/yinxiangpingfan/cc-mini-go/log"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

func TestAgent(t *testing.T) {
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
	res, err := a.Agent([]client.Message{
		*cm.NewUserMessage("你好，现在东京几点?以及告诉我/Users/easyimpr/Desktop/cc-mini-go/test/agent_test.go里面的内容"),
	}, prompt.SystemPrompt)
	if err != nil {
		t.Error(err)
	}
	for _, choice := range res {
		t.Log(choice)
	}
}

func TestAgentStream(t *testing.T) {
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
		*cm.NewUserMessage("你好，现在东京几点啊，并且告诉我/Users/easyimpr/Desktop/cc-mini-go/agent_tools/read.go里面的内容？"),
	}, prompt.SystemPrompt)
	if err != nil {
		t.Error(err)
	}
	for _, choice := range res {
		t.Log(choice)
	}
}
