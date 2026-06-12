package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/yinxiangpingfan/cc-mini-go/client"
)

func TestNewProviderSelectsProtocol(t *testing.T) {
	cfg := ProviderConfig{
		BaseURL: "https://example.com",
		APIKey:  "sk-test-key-with-enough-length",
		Model:   "test-model",
	}
	tests := []struct {
		name     string
		protocol string
		wantType any
	}{
		{name: "anthropic", protocol: "anthropic", wantType: &AnthropicProvider{}},
		{name: "openai-chat", protocol: "openai-chat", wantType: &OpenAIChatProvider{}},
		{name: "openai-responses", protocol: "openai-responses", wantType: &OpenAIResponsesProvider{}},
		{name: "empty defaults to openai-chat", protocol: "", wantType: &OpenAIChatProvider{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewProvider(cfg, tt.protocol)
			if err != nil {
				t.Fatalf("NewProvider() error = %v", err)
			}
			switch tt.wantType.(type) {
			case *AnthropicProvider:
				if _, ok := got.(*AnthropicProvider); !ok {
					t.Fatalf("got %T, want *AnthropicProvider", got)
				}
			case *OpenAIChatProvider:
				if _, ok := got.(*OpenAIChatProvider); !ok {
					t.Fatalf("got %T, want *OpenAIChatProvider", got)
				}
			case *OpenAIResponsesProvider:
				if _, ok := got.(*OpenAIResponsesProvider); !ok {
					t.Fatalf("got %T, want *OpenAIResponsesProvider", got)
				}
			}
		})
	}
}

func TestNewProviderRejectsUnknownProtocol(t *testing.T) {
	cfg := ProviderConfig{
		BaseURL: "https://example.com",
		APIKey:  "sk-test",
		Model:   "test",
	}
	_, err := NewProvider(cfg, "unknown-protocol")
	if err == nil {
		t.Fatal("NewProvider() error = nil, want error")
	}
}

func TestNewProviderWithHTTPClient(t *testing.T) {
	cfg := ProviderConfig{
		BaseURL: "https://example.com",
		APIKey:  "sk-test",
		Model:   "test",
	}
	customClient := &http.Client{}
	p, err := NewProviderWithHTTPClient(cfg, "openai-chat", customClient)
	if err != nil {
		t.Fatalf("NewProviderWithHTTPClient() error = %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestModelOrDefault(t *testing.T) {
	if got := modelOrDefault("primary", "fallback"); got != "primary" {
		t.Fatalf("modelOrDefault = %q, want primary", got)
	}
	if got := modelOrDefault("", "fallback"); got != "fallback" {
		t.Fatalf("modelOrDefault = %q, want fallback", got)
	}
}

func TestProviderError(t *testing.T) {
	original := NewProviderError(429, "rate limited", nil)
	if original.StatusCode != 429 {
		t.Fatalf("StatusCode = %d, want 429", original.StatusCode)
	}
	if original.Error() != "rate limited (status 429)" {
		t.Fatalf("Error() = %q", original.Error())
	}

	// Unwrap returns nil when no underlying error
	if original.Unwrap() != nil {
		t.Fatal("Unwrap() should return nil")
	}
}

// ---- Conversion function tests ----

func TestToAnthropicTools(t *testing.T) {
	tools := []client.Tool{
		{
			Type: "function",
			Function: client.FunctionDefinition{
				Name:        "get_weather",
				Description: "Get weather for a city",
				Parameters: client.FunctionParameters{
					Type: "object",
					Properties: map[string]any{
						"city": map[string]any{"type": "string"},
					},
					Required: []string{"city"},
				},
			},
		},
	}

	result := toAnthropicTools(tools)
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	if result[0].OfTool == nil {
		t.Fatal("OfTool is nil")
	}
	if result[0].OfTool.Name != "get_weather" {
		t.Fatalf("Name = %q, want get_weather", result[0].OfTool.Name)
	}
	if result[0].OfTool.InputSchema.Type != "object" {
		t.Fatalf("InputSchema.Type = %q, want object", result[0].OfTool.InputSchema.Type)
	}
}

func TestExtractAnthropicSystem(t *testing.T) {
	messages := []any{
		&client.Message{Role: "system", Content: "prompt 1"},
		&client.Message{Role: "user", Content: "hello"},
		&client.Message{Role: "system", Content: "prompt 2"},
	}
	system := extractAnthropicSystem(messages)
	if len(system) != 2 {
		t.Fatalf("len = %d, want 2", len(system))
	}
	if system[0].Text != "prompt 1" || system[1].Text != "prompt 2" {
		t.Fatalf("system texts = %v", system)
	}
}

func TestToAnthropicMessages(t *testing.T) {
	messages := []any{
		&client.Message{Role: "user", Content: "hello"},
		&client.Message{Role: "assistant", Content: "hi there"},
	}
	result := toAnthropicMessages(messages)
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if result[0].Role != "user" {
		t.Fatalf("first role = %s, want user", result[0].Role)
	}
	if result[1].Role != "assistant" {
		t.Fatalf("second role = %s, want assistant", result[1].Role)
	}
}

func TestToAnthropicMessages_WithToolCalls(t *testing.T) {
	messages := []any{
		&client.Message{Role: "user", Content: "read a.txt"},
		&client.ResponseMessage{
			Role:    "assistant",
			Content: nil,
			ToolCalls: []client.ToolCall{
				{Id: "call_1", Type: "function", Function: client.FunctionCall{Name: "read_file", Arguments: `{"file_path":"a.txt"}`}},
			},
		},
		&client.ToolsMessage{Role: "tool", ToolsId: "call_1", Content: `{"content":"hello"}`},
	}

	result := toAnthropicMessages(messages)
	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}
	// First: user message
	if result[0].Role != "user" {
		t.Fatalf("first role = %s, want user", result[0].Role)
	}
	// Second: assistant with tool_use
	if result[1].Role != "assistant" {
		t.Fatalf("second role = %s, want assistant", result[1].Role)
	}
	// Third: user with tool_result (wrapped because tool results go in user messages)
	if result[2].Role != "user" {
		t.Fatalf("third role = %s, want user (tool_result)", result[2].Role)
	}
}

func TestMergeConsecutiveSameRole(t *testing.T) {
	messages := []anthropic.MessageParam{
		{Role: "user", Content: []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock("a")}},
		{Role: "user", Content: []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock("b")}},
		{Role: "assistant", Content: []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock("c")}},
	}
	result := mergeConsecutiveSameRole(messages)
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if len(result[0].Content) != 2 {
		t.Fatalf("merged content len = %d, want 2", len(result[0].Content))
	}
}

func TestFinishReasonFromAnthropic(t *testing.T) {
	tests := []struct {
		reason anthropic.StopReason
		want   string
	}{
		{anthropic.StopReasonEndTurn, "stop"},
		{anthropic.StopReasonMaxTokens, "length"},
		{anthropic.StopReasonToolUse, "tool_calls"},
		{anthropic.StopReasonStopSequence, "stop"},
	}
	for _, tt := range tests {
		t.Run(string(tt.reason), func(t *testing.T) {
			if got := finishReasonFromAnthropic(tt.reason); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToOpenAIChatMessages(t *testing.T) {
	messages := []any{
		&client.Message{Role: "system", Content: "you are helpful"},
		&client.Message{Role: "user", Content: "hello"},
		&client.Message{Role: "assistant", Content: "hi"},
		&client.ToolsMessage{Role: "tool", ToolsId: "call_1", Content: "result"},
	}
	result := toOpenAIChatMessages(messages)
	if len(result) != 4 {
		t.Fatalf("len = %d, want 4", len(result))
	}
}

func TestToOpenAIChatMessages_WithToolCalls(t *testing.T) {
	messages := []any{
		&client.ResponseMessage{
			Role:    "assistant",
			Content: nil,
			ToolCalls: []client.ToolCall{
				{Id: "call_1", Type: "function", Function: client.FunctionCall{Name: "read", Arguments: `{}`}},
			},
		},
	}
	result := toOpenAIChatMessages(messages)
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	if result[0].OfAssistant == nil {
		t.Fatal("OfAssistant is nil")
	}
	if len(result[0].OfAssistant.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(result[0].OfAssistant.ToolCalls))
	}
}

func TestToOpenAIChatTools(t *testing.T) {
	tools := []client.Tool{
		{
			Type: "function",
			Function: client.FunctionDefinition{
				Name:        "test_tool",
				Description: "a test",
				Parameters: client.FunctionParameters{
					Type: "object",
					Properties: map[string]any{
						"x": map[string]any{"type": "integer"},
					},
					Required: []string{"x"},
				},
			},
		},
	}
	result := toOpenAIChatTools(tools)
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
}

func TestToResponsesInput(t *testing.T) {
	messages := []any{
		&client.Message{Role: "user", Content: "hello"},
		&client.Message{Role: "assistant", Content: "hi"},
	}
	result := toResponsesInput(messages)
	if len(result.OfInputItemList) != 2 {
		t.Fatalf("len = %d, want 2", len(result.OfInputItemList))
	}
}

func TestToResponsesInput_Empty(t *testing.T) {
	result := toResponsesInput(nil)
	if result.OfString.Value != "" {
		t.Fatalf("empty input should have empty string")
	}
}

func TestMsgContentString(t *testing.T) {
	if got := msgContentString("hello"); got != "hello" {
		t.Fatalf("string: %q", got)
	}
	if got := msgContentString(nil); got != "" {
		t.Fatalf("nil: %q", got)
	}
	if got := msgContentString(42); got != "42" {
		t.Fatalf("int: %q", got)
	}
}

func TestJoinStrings(t *testing.T) {
	if got := joinStrings([]string{"a", "b", "c"}, ", "); got != "a, b, c" {
		t.Fatalf("got %q", got)
	}
	if got := joinStrings([]string{}, ", "); got != "" {
		t.Fatalf("empty: %q", got)
	}
	if got := joinStrings([]string{"single"}, ", "); got != "single" {
		t.Fatalf("single: %q", got)
	}
}

// ---- OpenAIChatProvider tests ----

func TestOpenAIChatProvider_NonStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		resp := map[string]any{
			"id":    "chat-123",
			"model": "gpt-4o",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "Hello, world!",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 3,
				"total_tokens":      13,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := ProviderConfig{
		BaseURL: srv.URL + "/",
		APIKey:  "sk-test",
		Model:   "gpt-4o",
	}
	p := NewOpenAIChatProvider(cfg, nil)

	resp, err := p.CreateChatCompletion(context.Background(), &ChatRequest{
		Model: "gpt-4o",
		Messages: []any{
			&client.Message{Role: "user", Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("CreateChatCompletion() error = %v", err)
	}
	if resp.Message == nil {
		t.Fatal("Message is nil")
	}
	if resp.Message.Content.(string) != "Hello, world!" {
		t.Fatalf("Content = %q", resp.Message.Content)
	}
	if resp.FinishReason != "stop" {
		t.Fatalf("FinishReason = %q", resp.FinishReason)
	}
	if resp.Usage.TotalTokens != 13 {
		t.Fatalf("TotalTokens = %d", resp.Usage.TotalTokens)
	}
}

func TestOpenAIChatProvider_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{"error": "unauthorized"})
	}))
	defer srv.Close()

	cfg := ProviderConfig{
		BaseURL: srv.URL + "/",
		APIKey:  "bad-key",
		Model:   "gpt-4o",
	}
	p := NewOpenAIChatProvider(cfg, nil)

	_, err := p.CreateChatCompletion(context.Background(), &ChatRequest{
		Model:    "gpt-4o",
		Messages: []any{&client.Message{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---- AnthropicProvider tests ----
func TestAnthropicProvider_WithToolUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":   "msg-456",
			"model": "claude-sonnet-4-20250514",
			"role":  "assistant",
			"content": []map[string]any{
				{"type": "text", "text": "Let me read that file."},
				{"type": "tool_use", "id": "toolu_001", "name": "read_file", "input": map[string]any{"file_path": "/test.txt"}},
			},
			"stop_reason": "tool_use",
			"usage":        map[string]any{"input_tokens": 20, "output_tokens": 30},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := ProviderConfig{
		BaseURL: srv.URL + "/v1/",
		APIKey:  "sk-ant-test",
		Model:   "claude-sonnet-4-20250514",
	}
	p := NewAnthropicProvider(cfg, nil)

	resp, err := p.CreateChatCompletion(context.Background(), &ChatRequest{
		Model:    "claude-sonnet-4-20250514",
		Messages: []any{&client.Message{Role: "user", Content: "read /test.txt"}},
	})
	if err != nil {
		t.Fatalf("CreateChatCompletion() error = %v", err)
	}
	if resp.Message == nil {
		t.Fatal("Message is nil")
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.Message.ToolCalls))
	}
	if resp.Message.ToolCalls[0].Id != "toolu_001" {
		t.Fatalf("tool call id = %q", resp.Message.ToolCalls[0].Id)
	}
	if resp.Message.ToolCalls[0].Function.Name != "read_file" {
		t.Fatalf("tool name = %q", resp.Message.ToolCalls[0].Function.Name)
	}
	if resp.FinishReason != "tool_calls" {
		t.Fatalf("FinishReason = %q, want tool_calls", resp.FinishReason)
	}
}

func TestOpenAIChatProvider_Streaming(t *testing.T) {
	t.Skip("httptest SSE with OpenAI SDK requires chunked transfer encoding")
}
func TestAnthropicProvider_Streaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		events := []string{
			`event: message_start
data: {"type":"message_start","message":{"id":"msg_123","model":"claude-sonnet-4-20250514","type":"message","role":"assistant","content":[],"usage":{"input_tokens":10,"output_tokens":0}}}`,
			`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" Claude"}}`,
			`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
			`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
			`event: message_stop
data: {"type":"message_stop"}`,
		}
		for _, e := range events {
			w.Write([]byte(e + "\n\n"))
			flusher.Flush()
		}
	}))
	defer srv.Close()

	cfg := ProviderConfig{
		BaseURL: srv.URL + "/v1/",
		APIKey:  "sk-ant-test",
		Model:   "claude-sonnet-4-20250514",
	}
	p := NewAnthropicProvider(cfg, nil)

	var fullText string
	err := p.CreateChatCompletionStream(context.Background(), &ChatRequest{
		Model:    "claude-sonnet-4-20250514",
		Messages: []any{&client.Message{Role: "user", Content: "hi"}},
	}, func(evt StreamEvent) {
		switch evt.Type {
		case StreamEventContentDelta:
			fullText += evt.Delta
		}
	})
	if err != nil {
		t.Fatalf("CreateChatCompletionStream() error = %v", err)
	}
	if fullText != "Hello Claude" {
		t.Fatalf("fullText = %q, want 'Hello Claude'", fullText)
	}
}

func TestAnthropicProvider_StreamingWithToolUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		events := []string{
			`event: message_start
data: {"type":"message_start","message":{"id":"msg","model":"claude","role":"assistant","content":[],"usage":{"input_tokens":5,"output_tokens":0}}}`,
			`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_abc","name":"read_file","input":{}}}`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"file"}}`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"_path\":\"/a.txt\"}"}}`,
			`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
			`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":10}}`,
			`event: message_stop
data: {"type":"message_stop"}`,
		}
		for _, e := range events {
			w.Write([]byte(e + "\n\n"))
			flusher.Flush()
		}
	}))
	defer srv.Close()

	cfg := ProviderConfig{
		BaseURL: srv.URL + "/v1/",
		APIKey:  "sk-ant-test",
		Model:   "claude-sonnet-4-20250514",
	}
	p := NewAnthropicProvider(cfg, nil)

	var toolName, toolID, toolArgs string
	err := p.CreateChatCompletionStream(context.Background(), &ChatRequest{
		Model:    "claude-sonnet-4-20250514",
		Messages: []any{&client.Message{Role: "user", Content: "read a.txt"}},
	}, func(evt StreamEvent) {
		switch evt.Type {
		case StreamEventToolCallBegin:
			toolName = evt.ToolCallName
			toolID = evt.ToolCallID
		case StreamEventToolCallDelta:
			toolArgs += evt.Delta
		}
	})
	if err != nil {
		t.Fatalf("CreateChatCompletionStream() error = %v", err)
	}
	if toolName != "read_file" {
		t.Fatalf("toolName = %q", toolName)
	}
	if toolID != "toolu_abc" {
		t.Fatalf("toolID = %q", toolID)
	}
	if toolArgs != `{"file_path":"/a.txt"}` {
		t.Fatalf("toolArgs = %q", toolArgs)
	}
}
