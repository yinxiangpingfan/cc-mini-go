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
		*cm.NewUserMessage("你好，现在东京几点啊，并且在/Users/easyimpr/Desktop/cc-mini-go/test/data目录下创建一个test.txt文件，内容为test"),
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
		*cm.NewUserMessage("请你查看/Users/easyimpr/Desktop/cc-mini-go/client和/Users/easyimpr/Desktop/cc-mini-go/tools目录下的文件，告诉我这俩个包是干啥的，请你启动subagent实现这个功能。"),
	}, prompt.SystemPrompt)
	if err != nil {
		t.Error(err)
	}
	for _, choice := range res {
		t.Log(choice)
	}
}
