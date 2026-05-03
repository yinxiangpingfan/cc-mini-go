package client

import (
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/config"
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
	res, resp, err := NewCallRequest(cl.httpClient, cl.baseUrl, cl.apiKey, config.Model, []Message{
		{Role: "user", Content: "你是谁"},
	}, false, "你是一个奶龙")
	if err != nil {
		t.Error(err)
	}
	t.Logf("HTTP Status: %d", resp.StatusCode)
	t.Logf("Response: %+v", res)
}
