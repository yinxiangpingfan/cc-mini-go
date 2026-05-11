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
	res, resp, err := client.NewCall(cl, cm).NewCallRequest(config.Model, []any{
		client.Message{Role: "user", Content: "你可以干啥"},
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
	res, resp, err := client.NewCall(cl, cm).NewCallRequest(config.Model, []any{
		client.Message{Role: "user", Content: "你好，你可以干啥"},
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
	tool := tools.NewTimeNowTool()
	cm := client.NewChatCompletionMessage()
	res, resp, err := client.NewCall(cl, cm).NewCallRequest(config.Model, []any{
		client.Message{Role: "user", Content: "你好，我在东京现在几点了"},
	}, false, prompt.SystemPrompt, []client.Tool{tool.TimeNowInfoForLLm()}, nil)
	if err != nil {
		if errors.Is(err, io.EOF) {
			t.Log("Stream completed")
		} else {
			t.Error(err)
		}
	}
	if len(res.Choices[0].Message.ToolCalls) > 0 {
		switch res.Choices[0].Message.ToolCalls[0].Function.Name {
		case tool.Name:
			t.Log("Tool call: ", res.Choices[0].Message.ToolCalls[0].Function.Arguments)
			var args map[string]interface{}
			json.Unmarshal([]byte(res.Choices[0].Message.ToolCalls[0].Function.Arguments), &args)
			res := tool.Func(args)
			t.Log("Tool call result: ", res)
		}
	}
	t.Logf("HTTP Status: %d", resp.StatusCode)
	t.Logf("Response: %+v", res)
}

func TestCallWithToolStream(t *testing.T) {
	config, err := config.GetConfig()
	if err != nil {
		t.Error(err, "Failed to get config")
	}
	t.Log("Init client with api url: " + config.ApiUrl)
	cl, err := client.Init(config.ApiUrl, config.ApiKey)
	if err != nil {
		t.Error(err)
	}
	tool := tools.NewTimeNowTool()
	cm := client.NewChatCompletionMessage()
	activeToolCalls := make(map[int]*client.StreamToolCall)
	res, resp, err := client.NewCall(cl, cm).NewCallRequest(config.Model, []any{
		client.Message{Role: "user", Content: "你好，现在东京几点"},
	}, true, prompt.SystemPrompt, []client.Tool{tool.TimeNowInfoForLLm()}, func(sr client.StreamResponse) {
		if sr.Choices[0].Delta.Content != "" {
			fmt.Print(sr.Choices[0].Delta.Content)
		}
		if len(sr.Choices[0].Delta.ToolCalls) > 0 {
			for _, tc := range sr.Choices[0].Delta.ToolCalls {
				idx := tc.Index
				if _, exists := activeToolCalls[idx]; !exists {
					activeToolCalls[idx] = &client.StreamToolCall{}
				}
				// 获取当前这个 Index 对应的本地缓存对象
				activeCall := activeToolCalls[idx]

				// 组装 ID (仅首次出现时 tcChunk.ID 有值)
				if tc.Id != nil {
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
	})
	if err != nil {
		if errors.Is(err, io.EOF) {
			t.Log("Stream completed")
		} else {
			t.Error(err)
		}
	}
	for _, activeCall := range activeToolCalls {
		if activeCall.Function != nil && activeCall.Function.Arguments != nil {
			t.Log("Tool call: ", *activeCall.Function.Arguments)
			var args map[string]interface{}
			json.Unmarshal([]byte(*activeCall.Function.Arguments), &args)
			res := tool.Func(args)
			t.Log("Tool call result: ", res)
		}
	}
	t.Logf("HTTP Status: %d", resp.StatusCode)
	t.Logf("Response: %+v", res)
}
