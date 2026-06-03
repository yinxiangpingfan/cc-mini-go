package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
)

type Call struct {
	cl  *ChatCompletionClient
	Cm  *ChatCompletionMessage
	log *slog.Logger
}

func NewCall(cl *ChatCompletionClient, cm *ChatCompletionMessage, log *slog.Logger) *Call {
	return &Call{
		cl:  cl,
		Cm:  cm,
		log: log,
	}
}

// NewCallRequest 不带 context 的版本，等价于使用 context.Background()，保留以兼容现有调用方。
func (c *Call) NewCallRequest(model string, messages []any, stream bool, system string, tools []Tool, streamMessageFunc func(StreamResponse)) (CallResponse, *http.Response, error) {
	return c.NewCallRequestCtx(context.Background(), model, messages, stream, system, tools, streamMessageFunc)
}

// NewCallRequestCtx 带 context 的请求：取消 ctx 会中断进行中的 HTTP 请求（含流式读取）。
func (c *Call) NewCallRequestCtx(ctx context.Context, model string, messages []any, stream bool, system string, tools []Tool, streamMessageFunc func(StreamResponse)) (CallResponse, *http.Response, error) {
	// If stream is true, use the newCallRequestWithStream method.
	if stream {
		return c.newCallRequestWithStream(ctx, model, messages, system, tools, streamMessageFunc)
	}
	allMsgs := make([]any, 0, 1+len(messages))
	allMsgs = append(allMsgs, *c.Cm.NewSystemMessage(system))
	reqBody := CallRequest{
		Model:    model,
		Messages: append(allMsgs, messages...),
		Stream:   stream,
		Tools:    tools,
	}
	// 仅在确实提供了工具时设置 tool_choice，否则部分服务端会因 "tool_choice 但无 tools" 报错
	if len(tools) > 0 {
		reqBody.ToolChoice = "auto"
	}
	reqBodyJson, err := json.Marshal(reqBody)
	if err != nil {
		return CallResponse{}, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.cl.baseUrl+"/chat/completions", bytes.NewReader(reqBodyJson))
	if err != nil {
		return CallResponse{}, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cl.apiKey)
	resp, err := c.cl.httpClient.Do(req)
	if err != nil {
		return CallResponse{}, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return CallResponse{}, nil, err
	}
	var callResponse CallResponse
	err = json.Unmarshal(body, &callResponse)
	return callResponse, resp, err
}

// newCallRequestWithStream creates a new call request to the OpenAI API with streaming enabled.
func (c *Call) newCallRequestWithStream(ctx context.Context, model string, messages []any, system string, tools []Tool, onMessage func(StreamResponse)) (CallResponse, *http.Response, error) {
	type openaiReq struct {
		Model    string `json:"model"`
		Messages []any  `json:"messages"`
		Stream   bool   `json:"stream"`
		Tools    []Tool `json:"tools,omitempty"`
	}
	allMsgs := make([]any, 0, 1+len(messages))
	allMsgs = append(allMsgs, *c.Cm.NewSystemMessage(system))
	reqBody := CallRequest{
		Model:      model,
		Messages:   append(allMsgs, messages...),
		Stream:     true,
		Tools:      tools,
		ToolChoice: "auto",
	}
	reqBodyJson, err := json.Marshal(reqBody)
	if err != nil {
		return CallResponse{}, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.cl.baseUrl+"/chat/completions", bytes.NewReader(reqBodyJson))
	if err != nil {
		return CallResponse{}, nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cl.apiKey)
	resp, err := c.cl.httpClient.Do(req)
	if err != nil {
		return CallResponse{}, nil, err
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if bytes.HasPrefix(line, []byte("data: ")) {
			line = bytes.TrimPrefix(line, []byte("data: "))
		}
		if string(line) == "[DONE]" {
			return CallResponse{}, resp, io.EOF
		}
		var streamResp StreamResponse
		err := json.Unmarshal(line, &streamResp)
		if err != nil {
			return CallResponse{}, resp, err
		}
		// 调用传入的回调函数处理流响应
		onMessage(streamResp)
	}
	// 扫描结束：区分正常结束与读取错误（如 ctx 取消会让 Body 读取返回错误）
	if err := scanner.Err(); err != nil {
		return CallResponse{}, resp, err
	}
	return CallResponse{}, resp, nil
}
