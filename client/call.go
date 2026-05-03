package client

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

func NewCallRequest(cl *ChatCompletionClient, cm *ChatCompletionMessage, model string, messages []Message, stream bool, system string) (CallResponse, *http.Response, error) {
	type openaiReq struct {
		Model    string    `json:"model"`
		Messages []Message `json:"messages"`
		Stream   bool      `json:"stream"`
	}
	systemMsg := make([]Message, 1)
	systemMsg[0] = *cm.NewSystemMessage(system)
	reqBody := openaiReq{
		Model:    model,
		Messages: append(systemMsg, messages...),
		Stream:   stream,
	}
	reqBodyJson, err := json.Marshal(reqBody)
	if err != nil {
		return CallResponse{}, nil, err
	}

	req, err := http.NewRequest("POST", cl.baseUrl+"/chat/completions", bytes.NewReader(reqBodyJson))
	if err != nil {
		return CallResponse{}, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cl.apiKey)
	resp, err := cl.httpClient.Do(req)
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
