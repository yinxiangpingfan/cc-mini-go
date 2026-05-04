package test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/config"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
	"github.com/yinxiangpingfan/cc-mini-go/tools"
)

func TestCall(t *testing.T) {
	config, err := config.GetConfig()
	if err != nil {
		t.Error(err, "Failed to get config")
	}
	t.Log("Init client with api url: " + config.ApiUrl)
	cl, err := client.Init(config.ApiUrl, config.ApiKey)
	if err != nil {
		t.Error(err)
	}
	cm := client.NewChatCompletionMessage()
	res, resp, err := client.NewCall(cl, cm).NewCallRequest(config.Model, []client.Message{
		{Role: "user", Content: "你可以干啥"},
	}, false, prompt.SystemPrompt, nil, nil)
	if err != nil {
		t.Error(err)
	}
	t.Logf("HTTP Status: %d", resp.StatusCode)
	t.Logf("Response: %+v", res)
}

func TestCallStream(t *testing.T) {
	config, err := config.GetConfig()
	if err != nil {
		t.Error(err, "Failed to get config")
	}
	t.Log("Init client with api url: " + config.ApiUrl)
	cl, err := client.Init(config.ApiUrl, config.ApiKey)
	if err != nil {
		t.Error(err)
	}
	cm := client.NewChatCompletionMessage()
	res, resp, err := client.NewCall(cl, cm).NewCallRequest(config.Model, []client.Message{
		{Role: "user", Content: "你好，你可以干啥"},
	}, true, prompt.SystemPrompt, nil, func(sr client.StreamResponse) {
		fmt.Println(sr)
	})
	if err != nil {
		if errors.Is(err, io.EOF) {
			t.Log("Stream completed")
		} else {
			t.Error(err)
		}
	}
	t.Logf("HTTP Status: %d", resp.StatusCode)
	t.Logf("Response: %+v", res)
}

func TestCallWithTool(t *testing.T) {
	config, err := config.GetConfig()
	if err != nil {
		t.Error(err, "Failed to get config")
	}
	t.Log("Init client with api url: " + config.ApiUrl)
	cl, err := client.Init(config.ApiUrl, config.ApiKey)
	if err != nil {
		t.Error(err)
	}
	tool := tools.TimeNowTool()
	cm := client.NewChatCompletionMessage()
	res, resp, err := client.NewCall(cl, cm).NewCallRequest(config.Model, []client.Message{
		{Role: "user", Content: "你好，我在东京现在几点了"},
	}, false, prompt.SystemPrompt, []client.Tool{tool}, nil)
	if err != nil {
		if errors.Is(err, io.EOF) {
			t.Log("Stream completed")
		} else {
			t.Error(err)
		}
	}
	if len(res.Choices[0].Message.ToolCalls) > 0 {
		switch res.Choices[0].Message.ToolCalls[0].Function.Name {
		case "time_now":
			t.Log("Tool call: ", res.Choices[0].Message.ToolCalls[0].Function.Arguments)
			var args map[string]interface{}
			json.Unmarshal([]byte(res.Choices[0].Message.ToolCalls[0].Function.Arguments), &args)
			res, err := tools.TimeNowToolUse(args)
			if err != nil {
				t.Error(err)
			}
			t.Log("Tool call result: ", res)
		}
	}
	t.Logf("HTTP Status: %d", resp.StatusCode)
	t.Logf("Response: %+v", res)
}
