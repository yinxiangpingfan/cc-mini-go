package agent

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"math/rand/v2"
	"time"

	"github.com/yinxiangpingfan/cc-mini-go/client"
	perrors "github.com/yinxiangpingfan/cc-mini-go/errors"
)

// agentMaxTurns agent 主循环最大轮次，防止工具调用陷入无限循环
const agentMaxTurns = 200

// 以下为重试调参，使用包级变量以便测试调小；运行时不应修改。
var (
	// maxLLMRetries 单次 LLM 调用的最大重试次数（不含首次）
	maxLLMRetries = 4
	// baseRetryDelay 指数退避的基准时延
	baseRetryDelay = 500 * time.Millisecond
	// maxRetryDelay 退避时延上限
	maxRetryDelay = 8 * time.Second
)

// isRetryableStatusCode 判断 HTTP 状态码是否值得重试。
// 仅对限流(429)与服务端临时故障(5xx/408)重试；
// 4xx 客户端错误（鉴权失败、请求非法等）属于确定性错误，直接失败不重试。
func isRetryableStatusCode(code int) bool {
	switch code {
	case 408, 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

// backoffDelay 计算第 attempt 次重试前的等待时长：
// 指数退避（base * 2^(attempt-1)）+ ±25% 抖动，封顶 maxRetryDelay。
func backoffDelay(attempt int) time.Duration {
	d := baseRetryDelay << (attempt - 1)
	if d <= 0 || d > maxRetryDelay {
		d = maxRetryDelay
	}
	jitter := time.Duration(rand.Int64N(int64(d)/2+1)) - d/4
	return d + jitter
}

// callWithRetry 封装非流式 LLM 调用：网络层错误与可重试状态码按指数退避自动重试；
// 不可重试的状态码（如 401/400）立即返回错误；重试耗尽返回聚合错误。
func (a *ChatCompletionAgent) callWithRetry(ctx context.Context, allMsg []any, system string, tools []client.Tool) (client.CallResponse, error) {
	var lastErr error
	for attempt := 0; attempt <= maxLLMRetries; attempt++ {
		if attempt > 0 {
			delay := backoffDelay(attempt)
			a.emit(AgentEvent{Type: EventRetry, Attempt: attempt, Max: maxLLMRetries, Delay: delay, Err: lastErr})
			// 退避等待期间也响应取消
			select {
			case <-ctx.Done():
				return client.CallResponse{}, ctx.Err()
			case <-time.After(delay):
			}
		}
		res, resp, err := a.call.NewCallRequestCtx(ctx, a.cf.Model, allMsg, false, system, tools, nil)
		// 先按状态码分类：拿到响应就以状态码为准（即使响应体解析失败）
		if resp != nil && resp.StatusCode != 200 {
			statusErr := fmt.Errorf(perrors.ErrHTTPStatusCode, resp.StatusCode)
			if !isRetryableStatusCode(resp.StatusCode) {
				return client.CallResponse{}, statusErr // 确定性错误，立即失败
			}
			lastErr = statusErr
			continue
		}
		// 无响应（连接失败）或 200 响应体解析失败：按网络层错误重试
		if err != nil {
			// 取消/超时不重试，直接返回
			if ctx.Err() != nil {
				return client.CallResponse{}, ctx.Err()
			}
			lastErr = fmt.Errorf("%w: %w", perrors.ErrLLMRequest, err)
			continue
		}
		return res, nil
	}
	return client.CallResponse{}, fmt.Errorf("%w (%d retries): %w", perrors.ErrLLMRetriesExhausted, maxLLMRetries, lastErr)
}

// streamWithRetry 封装流式 LLM 调用。reset 用于在重试前清空调用方累积的残留状态。
// 为避免已经向终端输出过内容后重试导致重复，只对两类失败重试：
//  1. 非 200 状态（错误响应体不是有效 SSE，不会产生真实输出）；
//  2. 连接尚未建立的网络错误（resp 为 nil）。
//
// 已建立 200 连接后中途出错（可能已输出部分内容）不重试，由调用方按错误返回处理。
func (a *ChatCompletionAgent) streamWithRetry(ctx context.Context, allMsg []any, system string, tools []client.Tool, reset func(), onMessage func(client.StreamResponse)) error {
	var lastErr error
	for attempt := 0; attempt <= maxLLMRetries; attempt++ {
		if attempt > 0 {
			reset()
			delay := backoffDelay(attempt)
			a.emit(AgentEvent{Type: EventRetry, Attempt: attempt, Max: maxLLMRetries, Delay: delay, Err: lastErr})
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		_, resp, err := a.call.NewCallRequestCtx(ctx, a.cf.Model, allMsg, true, system, tools, onMessage)

		// 取消优先：无论流处于哪个阶段，被取消就直接返回取消错误
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// 非 200：按状态码决定是否重试
		if resp != nil && resp.StatusCode != 200 {
			statusErr := fmt.Errorf(perrors.ErrHTTPStatusCode, resp.StatusCode)
			if !isRetryableStatusCode(resp.StatusCode) {
				return statusErr
			}
			lastErr = statusErr
			continue
		}
		// 网络层错误（EOF 表示流正常结束，不算错误）
		if err != nil && !stderrors.Is(err, io.EOF) {
			lastErr = fmt.Errorf("%w: %w", perrors.ErrLLMRequest, err)
			if resp == nil {
				continue // 连接未建立，重试安全
			}
			return lastErr // 连接已建立、可能已输出内容，不重试以免重复
		}
		return nil // 成功
	}
	return fmt.Errorf("%w (%d retries): %w", perrors.ErrLLMRetriesExhausted, maxLLMRetries, lastErr)
}

// toolErrorResult 把工具错误信息包成 LLM 可读的 JSON 串（与各工具的 jsonErr 同构）。
func toolErrorResult(msg string) string {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return string(b)
}

// safeToolCall 执行工具函数并捕获 panic：工具在 goroutine 中运行且无 recover，
// 任何 panic 都会拖垮整个 agent 进程。这里把 panic 转成普通错误结果回给 LLM，
// 保证单个工具的崩溃不影响其余工具与主循环。
func safeToolCall(name string, f func(map[string]any) string, args map[string]any) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = toolErrorResult(fmt.Sprintf("tool %q panicked: %v", name, r))
		}
	}()
	return f(args)
}
