package client

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

type Call struct {
	cl *ChatCompletionClient
	cm *ChatCompletionMessage
}

func NewCall(cl *ChatCompletionClient, cm *ChatCompletionMessage) *Call {
	return &Call{
		cl: cl,
		cm: cm,
	}
}

// NewCallRequest creates a new call request to the OpenAI API.
func (c *Call) NewCallRequest(model string, messages []Message, stream bool, system string, streamMessageFunc func(StreamResponse)) (CallResponse, *http.Response, error) {
	// If stream is true, use the newCallRequestWithStream method.
	if stream {
		return c.newCallRequestWithStream(model, messages, system, streamMessageFunc)
	}
	type openaiReq struct {
		Model    string    `json:"model"`
		Messages []Message `json:"messages"`
		Stream   bool      `json:"stream"`
	}
	systemMsg := make([]Message, 1)
	systemMsg[0] = *c.cm.NewSystemMessage(system)
	reqBody := openaiReq{
		Model:    model,
		Messages: append(systemMsg, messages...),
		Stream:   stream,
	}
	reqBodyJson, err := json.Marshal(reqBody)
	if err != nil {
		return CallResponse{}, nil, err
	}

	req, err := http.NewRequest("POST", c.cl.baseUrl+"/chat/completions", bytes.NewReader(reqBodyJson))
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
func (c *Call) newCallRequestWithStream(model string, messages []Message, system string, onMessage func(StreamResponse)) (CallResponse, *http.Response, error) {
	type openaiReq struct {
		Model    string    `json:"model"`
		Messages []Message `json:"messages"`
		Stream   bool      `json:"stream"`
	}
	systemMsg := make([]Message, 1)
	systemMsg[0] = *c.cm.NewSystemMessage(system)
	reqBody := openaiReq{
		Model:    model,
		Messages: append(systemMsg, messages...),
		Stream:   true,
	}
	reqBodyJson, err := json.Marshal(reqBody)
	if err != nil {
		return CallResponse{}, nil, err
	}

	req, err := http.NewRequest("POST", c.cl.baseUrl+"/chat/completions", bytes.NewReader(reqBodyJson))
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
	return CallResponse{}, resp, nil
}
