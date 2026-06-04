package errors

import "fmt"

var (
	// ErrInvalidBaseUrl 客户端初始化时 baseUrl 格式非法
	ErrInvalidBaseUrl = fmt.Errorf("invalid base url")
	// ErrHTTPStatusCode HTTP 请求返回非 200 状态码，使用时 fmt.Errorf(ErrHTTPStatusCode, statusCode)
	ErrHTTPStatusCode = "http request failed, status code: %d"
	// ErrLLMRequest LLM 请求在网络层失败
	ErrLLMRequest = fmt.Errorf("llm request failed")
	// ErrLLMRetriesExhausted 多次重试后 LLM 请求仍失败
	ErrLLMRetriesExhausted = fmt.Errorf("llm request failed after retries")
	// ErrAgentMaxTurns agent 主循环超过最大轮次上限（疑似无限循环）
	ErrAgentMaxTurns = fmt.Errorf("agent exceeded maximum turns")
	// ErrContextTooLong 请求上下文超过模型窗口；恢复路径是压缩后重试，而非退避或直接失败
	ErrContextTooLong = fmt.Errorf("context too long")
)
