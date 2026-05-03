package client

type ChatCompletionMessage struct{}

func NewChatCompletionMessage() *ChatCompletionMessage {
	return &ChatCompletionMessage{}
}

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

//============================Request=====================================

type CallRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	System   string    `json:"system,omitempty"`
}

//============================DefaultResponse=============================

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

func (m *ChatCompletionMessage) NewSystemMessage(content string) *Message {
	return &Message{
		Role:    "system",
		Content: content,
	}
}

func (m *ChatCompletionMessage) NewUserMessage(content string) *Message {
	return &Message{
		Role:    "user",
		Content: content,
	}
}
