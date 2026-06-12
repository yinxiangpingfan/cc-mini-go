// Package provider 定义统一的 LLM 供应商抽象层。
// 向上层（agent、TUI）提供协议无关的调用接口，
// 向下封装 Anthropic SDK 和 OpenAI SDK 的细节。
package provider

import (
	"context"
	"fmt"

	"github.com/yinxiangpingfan/cc-mini-go/client"
)

// ProviderError 携带 HTTP 状态码的供应商错误，方便 retry 层做状态码分类。
type ProviderError struct {
	StatusCode int
	Message    string
	Err        error
}

func (e *ProviderError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s (status %d): %v", e.Message, e.StatusCode, e.Err)
	}
	return fmt.Sprintf("%s (status %d)", e.Message, e.StatusCode)
}

func (e *ProviderError) Unwrap() error { return e.Err }

// NewProviderError 创建带状态码的 ProviderError。
// statusCode 为 0 表示无法确定（如网络错误）。
func NewProviderError(statusCode int, msg string, err error) *ProviderError {
	return &ProviderError{StatusCode: statusCode, Message: msg, Err: err}
}

// Provider 是统一的大模型供应商接口。
// Agent 循环只依赖此接口，不直接依赖任何具体 SDK。
type Provider interface {
	// CreateChatCompletion 发起非流式调用，返回完整响应。
	CreateChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

	// CreateChatCompletionStream 发起流式调用，通过回调输出增量事件。
	CreateChatCompletionStream(ctx context.Context, req *ChatRequest, cb StreamCallback) error
}

// ChatRequest 封装一次模型调用的全部参数。
type ChatRequest struct {
	Model    string        // 使用的模型名（为空时由供应商回退到配置中的默认值）
	Messages []any         // 规范格式消息列表：client.Message | client.ToolsMessage | client.ResponseMessage
	Tools    []client.Tool // 工具定义（OpenAI function-calling 格式作为规范）
	System   string        // 系统提示（提取出来，方便 Anthropic 使用独立的 System 参数）
}

// ChatResponse 封装一次非流式调用的完整返回。
type ChatResponse struct {
	Message      *client.ResponseMessage // 助手消息（含 tool_calls，如有）
	Usage        *client.Usage
	FinishReason string
}

// StreamEventType 区分流式事件的类型。
type StreamEventType string

const (
	StreamEventContentDelta  StreamEventType = "content_delta"   // 回复正文增量
	StreamEventThinkingDelta StreamEventType = "thinking_delta"  // thinking 内容增量（Claude extended thinking）
	StreamEventToolCallBegin StreamEventType = "tool_call_begin" // 开始一个新的工具调用块
	StreamEventToolCallDelta StreamEventType = "tool_call_delta" // 工具调用参数增量
	StreamEventDone          StreamEventType = "done"            // 本轮回复结束
	StreamEventError         StreamEventType = "error"           // 请求或解析失败
)

// StreamEvent 是供应商向 Agent 和 TUI 层输出的统一流式事件。
type StreamEvent struct {
	Type StreamEventType // 事件类型
	Delta string         // 增量文本（content_delta / thinking_delta / tool_call_delta）
	// tool_call 相关字段（仅在 tool_call_begin / tool_call_delta 时有效）
	ToolCallIndex int    // 工具调用序号（从 0 开始）
	ToolCallID    string // 工具调用 ID
	ToolCallName  string // 工具名称（仅在 tool_call_begin 时非空）
	Usage         *client.Usage // 仅在 done 事件前的最后一个事件中可能非空
	Err           error         // 错误事件携带的 error
}

// StreamCallback 是流式事件回调函数类型。
// 供应商在每次收到增量时调用此回调；当流结束时返回。
type StreamCallback func(event StreamEvent)
