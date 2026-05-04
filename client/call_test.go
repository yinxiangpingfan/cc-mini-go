package client

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/config"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

func TestCall(t *testing.T) {
	config, err := config.GetConfig()
	if err != nil {
		t.Error(err, "Failed to get config")
	}
	t.Log("Init client with api url: " + config.ApiUrl)
	cl, err := Init(config.ApiUrl, config.ApiKey)
	if err != nil {
		t.Error(err)
	}
	cm := NewChatCompletionMessage()
	res, resp, err := NewCall(cl, cm).NewCallRequest(config.Model, []Message{
		{Role: "user", Content: prompt.SystemPrompt},
	}, false, "你好，你可以干啥", nil)
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
	cl, err := Init(config.ApiUrl, config.ApiKey)
	if err != nil {
		t.Error(err)
	}
	cm := NewChatCompletionMessage()
	res, resp, err := NewCall(cl, cm).NewCallRequest(config.Model, []Message{
		{Role: "user", Content: prompt.SystemPrompt},
	}, true, "你好，你可以干啥", func(sr StreamResponse) {
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
