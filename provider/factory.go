package provider

import (
	"fmt"
	"net/http"
)

// NewProvider 根据配置的 Protocol 字段创建对应的 Provider。
func NewProvider(cfg ProviderConfig, protocol string) (Provider, error) {
	httpClient := newCleanHTTPClient()
	return NewProviderWithHTTPClient(cfg, protocol, httpClient)
}

// NewProviderWithHTTPClient 使用自定义 HTTP client 创建 Provider。
func NewProviderWithHTTPClient(cfg ProviderConfig, protocol string, httpClient *http.Client) (Provider, error) {
	wrapTransport(httpClient)
	switch protocol {
	case "anthropic":
		return NewAnthropicProvider(cfg, httpClient), nil
	case "openai-responses":
		return NewOpenAIResponsesProvider(cfg, httpClient), nil
	case "openai-chat", "":
		// 默认 OpenAI Chat Completions（向后兼容）
		return NewOpenAIChatProvider(cfg, httpClient), nil
	default:
		return nil, fmt.Errorf("不支持的协议 %q，可用: openai-chat, anthropic, openai-responses", protocol)
	}
}

// newCleanHTTPClient 创建带超时的 HTTP client。
func newCleanHTTPClient() *http.Client {
	return &http.Client{Timeout: defaultHTTPTimeout}
}

// wrapTransport 为 HTTP client 注入 header 清理 transport。
// OpenAI SDK 会发送 X-Stainless-* / User-Agent 等头，部分代理（如 coderelay.cn）
// 收到这些头会返回 403。清理后只保留 Authorization 和 Content-Type。
func wrapTransport(c *http.Client) {
	if c == nil {
		return
	}
	base := c.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	c.Transport = &cleanTransport{next: base}
}

type cleanTransport struct{ next http.RoundTripper }

func (t *cleanTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	auth := req.Header.Get("Authorization")
	ct := req.Header.Get("Content-Type")
	req.Header = make(http.Header)
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", ct)
	return t.next.RoundTrip(req)
}
