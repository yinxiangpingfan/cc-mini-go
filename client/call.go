package client

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

type CallRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	System   string    `json:"system,omitempty"`
}

type CallResponse struct {
	Id                string   `json:"id"`
	Object            string   `json:"object"`
	Created           int64    `json:"created"`
	Model             string   `json:"model"`
	Choices           []Choice `json:"choices"`
	Usage             Usage    `json:"usage"`
	ServiceTier       string   `json:"service_tier,omitempty"`
	SystemFingerprint string   `json:"system_fingerprint,omitempty"`
}

type Choice struct {
	Index        int             `json:"index"`
	Message      ResponseMessage `json:"message"`
	Logprobs     any             `json:"logprobs"`
	FinishReason string          `json:"finish_reason"`
}

type ResponseMessage struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Refusal   string     `json:"refusal,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	Id       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func NewCallRequest(httpClient *http.Client, inBaseUrl string, apiKey string, model string, messages []Message, stream bool, system string) (CallResponse, *http.Response, error) {
	type openaiReq struct {
		Model    string    `json:"model"`
		Messages []Message `json:"messages"`
		Stream   bool      `json:"stream"`
	}
	systemMsg := make([]Message, 1)
	systemMsg[0] = Message{
		Role:    "system",
		Content: system,
	}
	reqBody := openaiReq{
		Model:    model,
		Messages: append(systemMsg, messages...),
		Stream:   stream,
	}
	reqBodyJson, err := json.Marshal(reqBody)
	if err != nil {
		return CallResponse{}, nil, err
	}

	req, err := http.NewRequest("POST", inBaseUrl+"/chat/completions", bytes.NewReader(reqBodyJson))
	if err != nil {
		return CallResponse{}, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := httpClient.Do(req)
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
