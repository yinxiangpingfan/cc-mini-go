package client

type ChatCompletionMessage struct{}

func NewChatCompletionMessage() *ChatCompletionMessage {
	return &ChatCompletionMessage{}
}

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type ToolsMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	ToolsId string `json:"tool_call_id"`
}

//============================Request=====================================

type CallRequest struct {
	Model      string      `json:"model"`
	Messages   []any       `json:"messages"`
	Stream     bool        `json:"stream"`
	Tools      []Tool      `json:"tools,omitempty"`
	ToolChoice interface{} `json:"tool_choice,omitempty"` // "none"|"auto"|"required" 或 NamedToolChoice
}

type Tool struct {
	Type     string             `json:"type"` // "function"
	Function FunctionDefinition `json:"function"`
}

type FunctionDefinition struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Parameters  FunctionParameters `json:"parameters,omitempty"`
	Strict      bool               `json:"strict,omitempty"`
}

type FunctionParameters struct {
	Type       string                       `json:"type"` // "object"
	Properties map[string]ParameterProperty `json:"properties,omitempty"`
	Required   []string                     `json:"required,omitempty"`
}

type ParameterProperty struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
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
	Content   any        `json:"content"`
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

//============================StreamResponse==============================

type StreamResponse struct {
	Id                string         `json:"id"`
	Object            string         `json:"object"`
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	Choices           []StreamChoice `json:"choices"`
	SystemFingerprint string         `json:"system_fingerprint,omitempty"`
}

type StreamChoice struct {
	Index        int         `json:"index"`
	Delta        StreamDelta `json:"delta"`
	Logprobs     any         `json:"logprobs"`
	FinishReason string      `json:"finish_reason"`
}

type StreamDelta struct {
	Role             string           `json:"role,omitempty"`
	Content          string           `json:"content,omitempty"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ToolCalls        []StreamToolCall `json:"tool_calls,omitempty"`
}

type StreamToolCall struct {
	Index    int             `json:"index"`
	Type     string          `json:"type,omitempty"`
	Id       *string         `json:"id,omitempty"`
	Function *StreamFunction `json:"function"`
}

type StreamFunction struct {
	Name      *string `json:"name,omitempty"`
	Arguments *string `json:"arguments,omitempty"`
}

type ToolCallChunk struct {
	Index    int                `json:"index"`        // 唯一必须存在的字段
	ID       *string            `json:"id,omitempty"` // 使用指针处理 null 或未传
	Type     *string            `json:"type,omitempty"`
	Function *FunctionCallChunk `json:"function,omitempty"` // 指向具体 function 对象的指针
}

type FunctionCallChunk struct {
	Name      *string `json:"name,omitempty"`      // 函数名
	Arguments *string `json:"arguments,omitempty"` // 参数的增量字符串
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

func (m *ChatCompletionMessage) NewToolsCall(content any, call []ToolCall) *ResponseMessage {
	return &ResponseMessage{
		Role:      "assistant",
		Content:   content,
		ToolCalls: call,
	}
}

func (m *ChatCompletionMessage) NewToolsMessage(toolsId string, content string) *ToolsMessage {
	return &ToolsMessage{
		Role:    "tool",
		Content: content,
		ToolsId: toolsId,
	}
}

func (m *ChatCompletionMessage) NewAssistantMessage(content string) *Message {
	return &Message{
		Role:    "assistant",
		Content: content,
	}
}
