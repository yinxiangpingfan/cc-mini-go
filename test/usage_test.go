package test

import (
	"errors"
	"io"
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/config"
	"github.com/yinxiangpingfan/cc-mini-go/log"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

// TestUsage_NonStream 验证非流式响应带回 usage：prompt_tokens 是 token 预算的权威基线。
// 真打 API，依赖 ~/.cc_mini_go/setting.json。
func TestUsage_NonStream(t *testing.T) {
	cf, err := config.GetConfig()
	if err != nil {
		t.Fatal(err, "Failed to get config")
	}
	cl, err := client.Init(cf.ApiUrl, cf.ApiKey)
	if err != nil {
		t.Fatal(err)
	}
	call := client.NewCall(cl, client.NewChatCompletionMessage(), log.InitLogger())

	res, resp, err := call.NewCallRequest(cf.Model, []any{
		client.Message{Role: "user", Content: "用一句话回答：你好"},
	}, false, prompt.SystemPrompt, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("non-200: %d", resp.StatusCode)
	}

	u := res.Usage
	t.Logf("usage: prompt=%d completion=%d total=%d", u.PromptTokens, u.CompletionTokens, u.TotalTokens)
	if u.PromptTokens <= 0 {
		t.Fatal("prompt_tokens 应 > 0（token 预算的权威基线）")
	}
	if u.CompletionTokens <= 0 {
		t.Fatal("completion_tokens 应 > 0（模型这次的回答）")
	}
	// prompt 只数输入、completion 只数输出，total 是两者之和
	if u.TotalTokens != u.PromptTokens+u.CompletionTokens {
		t.Fatalf("total(%d) 应等于 prompt(%d)+completion(%d)", u.TotalTokens, u.PromptTokens, u.CompletionTokens)
	}
}

// TestUsage_Stream 验证流式开启 include_usage 后，末尾会有一个带 usage 的 chunk。
// 这是 TUI（走流式）能拿到权威 prompt_tokens 的前提。
func TestUsage_Stream(t *testing.T) {
	cf, err := config.GetConfig()
	if err != nil {
		t.Fatal(err, "Failed to get config")
	}
	cl, err := client.Init(cf.ApiUrl, cf.ApiKey)
	if err != nil {
		t.Fatal(err)
	}
	call := client.NewCall(cl, client.NewChatCompletionMessage(), log.InitLogger())

	var lastUsage *client.Usage
	usageChunks := 0
	_, _, err = call.NewCallRequest(cf.Model, []any{
		client.Message{Role: "user", Content: "用一句话回答：你好"},
	}, true, prompt.SystemPrompt, nil, func(sr client.StreamResponse) {
		if sr.Usage != nil {
			lastUsage = sr.Usage
			usageChunks++
		}
	})
	if err != nil && !errors.Is(err, io.EOF) { // 流正常结束以 EOF 表示
		t.Fatal(err)
	}

	if lastUsage == nil {
		t.Fatal("流式未收到 usage chunk —— 检查 stream_options.include_usage 是否生效/被服务端支持")
	}
	t.Logf("stream usage chunks=%d, prompt=%d completion=%d total=%d",
		usageChunks, lastUsage.PromptTokens, lastUsage.CompletionTokens, lastUsage.TotalTokens)
	if lastUsage.PromptTokens <= 0 {
		t.Fatal("流式 usage 的 prompt_tokens 应 > 0")
	}
}
