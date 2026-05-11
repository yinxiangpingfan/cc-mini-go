package errors

import "fmt"

var (
	// ErrInvalidBaseUrl 客户端初始化时 baseUrl 格式非法
	ErrInvalidBaseUrl = fmt.Errorf("invalid base url")
	// ErrHTTPStatusCode HTTP 请求返回非 200 状态码，使用时 fmt.Errorf(ErrHTTPStatusCode, statusCode)
	ErrHTTPStatusCode = "http request failed, status code: %d"
)
