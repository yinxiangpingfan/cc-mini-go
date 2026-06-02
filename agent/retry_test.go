package agent

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/config"
	perrors "github.com/yinxiangpingfan/cc-mini-go/errors"
)

// ---------- 测试基础设施 ----------

type mockResp struct {
	status int
	body   string
}

// mockLLM 按调用顺序返回预置响应，队列耗尽后重复最后一个。
type mockLLM struct {
	mu    sync.Mutex
	seq   []mockResp
	calls int
}

func (m *mockLLM) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.calls
	m.calls++
	resp := m.seq[len(m.seq)-1]
	if i < len(m.seq) {
		resp = m.seq[i]
	}
	if resp.status == 0 {
		resp.status = 200
	}
	w.WriteHeader(resp.status)
	_, _ = w.Write([]byte(resp.body))
}

func (m *mockLLM) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func newTestAgent(t *testing.T, url string) *ChatCompletionAgent {
	t.Helper()
	cl, err := client.Init(url, "test-key")
	if err != nil {
		t.Fatalf("client.Init: %v", err)
	}
	return &ChatCompletionAgent{
		cf:   &config.Config{Model: "test-model"},
		call: client.NewCall(cl, client.NewChatCompletionMessage(), nil),
	}
}

// fastRetries 把退避时延与重试次数调小，避免测试因 sleep 变慢。
func fastRetries(t *testing.T) {
	t.Helper()
	ob, om, on := baseRetryDelay, maxRetryDelay, maxLLMRetries
	baseRetryDelay = time.Millisecond
	maxRetryDelay = 2 * time.Millisecond
	maxLLMRetries = 3
	t.Cleanup(func() { baseRetryDelay, maxRetryDelay, maxLLMRetries = ob, om, on })
}

func finalRespBody(text string) string {
	b, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": text}}},
	})
	return string(b)
}

// ---------- emit（事件投递契约） ----------

func TestEmit_NilChannelIsNoop(t *testing.T) {
	a := &ChatCompletionAgent{} // events 为 nil
	// 不应 panic，纯空操作
	a.emit(AgentEvent{Type: EventContent, Text: "x"})
}

func TestEmit_DeliversThenDropsWhenFull(t *testing.T) {
	ch := make(chan AgentEvent, 1)
	a := &ChatCompletionAgent{events: ch}

	a.emit(AgentEvent{Type: EventContent, Text: "a"}) // 入缓冲
	a.emit(AgentEvent{Type: EventContent, Text: "b"}) // 缓冲已满 → 丢弃，且不得阻塞
	close(ch)

	var got []AgentEvent
	for ev := range ch {
		got = append(got, ev)
	}
	if len(got) != 1 || got[0].Text != "a" {
		t.Fatalf("expected only 'a' buffered (b dropped, non-blocking), got %+v", got)
	}
}

// ---------- isRetryableStatusCode ----------

func TestIsRetryableStatusCode(t *testing.T) {
	retry := []int{408, 429, 500, 502, 503, 504}
	for _, c := range retry {
		if !isRetryableStatusCode(c) {
			t.Errorf("status %d should be retryable", c)
		}
	}
	noRetry := []int{200, 201, 400, 401, 403, 404, 422}
	for _, c := range noRetry {
		if isRetryableStatusCode(c) {
			t.Errorf("status %d should NOT be retryable", c)
		}
	}
}

func TestBackoffDelay_PositiveAndCapped(t *testing.T) {
	for attempt := 1; attempt <= 10; attempt++ {
		d := backoffDelay(attempt)
		if d <= 0 {
			t.Fatalf("attempt %d: delay should be positive, got %v", attempt, d)
		}
		// 退避基准封顶 maxRetryDelay，再叠加 0~25% 抖动，故上界 = 1.25×maxRetryDelay
		max := time.Duration(float64(maxRetryDelay) * (1 + jitterFactor))
		if d > max {
			t.Fatalf("attempt %d: delay %v exceeds cap+jitter (%v)", attempt, d, max)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	mk := func(v string) http.Header {
		h := http.Header{}
		if v != "" {
			h.Set("Retry-After", v)
		}
		return h
	}
	if got := parseRetryAfter(mk("")); got != 0 {
		t.Fatalf("empty header should be 0, got %v", got)
	}
	if got := parseRetryAfter(nil); got != 0 {
		t.Fatalf("nil header should be 0, got %v", got)
	}
	if got := parseRetryAfter(mk("30")); got != 30*time.Second {
		t.Fatalf("'30' should be 30s, got %v", got)
	}
	if got := parseRetryAfter(mk("0")); got != 0 {
		t.Fatalf("'0' should be 0 (no wait), got %v", got)
	}
	if got := parseRetryAfter(mk("-5")); got != 0 {
		t.Fatalf("negative should be 0, got %v", got)
	}
	if got := parseRetryAfter(mk("garbage")); got != 0 {
		t.Fatalf("unparseable should be 0, got %v", got)
	}
	// HTTP-date 形式：未来 ~约定秒数，过去则为 0
	future := time.Now().Add(45 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(mk(future)); got <= 0 || got > 46*time.Second {
		t.Fatalf("future http-date should be ~45s, got %v", got)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(mk(past)); got != 0 {
		t.Fatalf("past http-date should be 0, got %v", got)
	}
}

func TestComputeRetryDelay_RetryAfterTakesPriority(t *testing.T) {
	// 有 Retry-After 时完全照办，不叠加退避/抖动
	if got := computeRetryDelay(3, 7*time.Second); got != 7*time.Second {
		t.Fatalf("Retry-After should be obeyed verbatim, got %v", got)
	}
	// 无 Retry-After 时退回退避（落在 [base, cap*1.25] 内）
	got := computeRetryDelay(1, 0)
	if got < baseRetryDelay || got > time.Duration(float64(maxRetryDelay)*(1+jitterFactor)) {
		t.Fatalf("fallback backoff out of range: %v", got)
	}
}

// ---------- safeToolCall ----------

func TestSafeToolCall_Passthrough(t *testing.T) {
	got := safeToolCall(context.Background(), "ok", func(ctx context.Context, m map[string]any) string { return `{"ok":true}` }, nil)
	if got != `{"ok":true}` {
		t.Fatalf("expected passthrough result, got: %s", got)
	}
}

func TestSafeToolCall_RecoversPanic(t *testing.T) {
	got := safeToolCall(context.Background(), "boom", func(ctx context.Context, m map[string]any) string { panic("kaboom") }, nil)
	var resp map[string]string
	if err := json.Unmarshal([]byte(got), &resp); err != nil {
		t.Fatalf("panic result not JSON: %v, raw: %s", err, got)
	}
	if !strings.Contains(resp["error"], "panicked") || !strings.Contains(resp["error"], "kaboom") {
		t.Fatalf("expected panic captured in error, got: %s", got)
	}
}

// ---------- callWithRetry ----------

func TestCallWithRetry_SucceedsAfterRetry(t *testing.T) {
	fastRetries(t)
	mock := &mockLLM{seq: []mockResp{{429, ""}, {200, finalRespBody("hello")}}}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	a := newTestAgent(t, srv.URL)
	res, err := a.callWithRetry(context.Background(), []any{}, "sys", nil)
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if mock.callCount() != 2 {
		t.Fatalf("expected 2 calls (429 then 200), got %d", mock.callCount())
	}
	if len(res.Choices) == 0 || res.Choices[0].Message.Content.(string) != "hello" {
		t.Fatalf("unexpected response: %+v", res)
	}
}

func TestCallWithRetry_EmitsRetryEvents(t *testing.T) {
	fastRetries(t)
	mock := &mockLLM{seq: []mockResp{{429, ""}, {500, ""}, {200, finalRespBody("ok")}}}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	ch := make(chan AgentEvent, 8)
	a := newTestAgent(t, srv.URL)
	a.events = ch

	if _, err := a.callWithRetry(context.Background(), []any{}, "sys", nil); err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	close(ch)

	var evs []AgentEvent
	for ev := range ch {
		evs = append(evs, ev)
	}
	// 429、500 各触发一次重试事件，第 3 次成功
	if len(evs) != 2 {
		t.Fatalf("expected 2 retry events, got %d: %+v", len(evs), evs)
	}
	if evs[0].Type != EventRetry || evs[0].Attempt != 1 || evs[0].Max != maxLLMRetries || evs[0].Err == nil {
		t.Fatalf("unexpected first retry event: %+v", evs[0])
	}
	if evs[1].Attempt != 2 || evs[1].Delay <= 0 {
		t.Fatalf("unexpected second retry event: %+v", evs[1])
	}
}

func TestCallWithRetry_FailsFastOnClientError(t *testing.T) {
	fastRetries(t)
	mock := &mockLLM{seq: []mockResp{{400, ""}}}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	a := newTestAgent(t, srv.URL)
	_, err := a.callWithRetry(context.Background(), []any{}, "sys", nil)
	if err == nil {
		t.Fatal("expected error on 400")
	}
	if mock.callCount() != 1 {
		t.Fatalf("400 must not be retried, expected 1 call, got %d", mock.callCount())
	}
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("expected status 400 in error, got: %v", err)
	}
}

func TestCallWithRetry_ExhaustsOnServerError(t *testing.T) {
	fastRetries(t)
	mock := &mockLLM{seq: []mockResp{{500, ""}}}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	a := newTestAgent(t, srv.URL)
	_, err := a.callWithRetry(context.Background(), []any{}, "sys", nil)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	// 首次 + maxLLMRetries 次重试
	if want := maxLLMRetries + 1; mock.callCount() != want {
		t.Fatalf("expected %d calls, got %d", want, mock.callCount())
	}
	if !stderrors.Is(err, perrors.ErrLLMRetriesExhausted) {
		t.Fatalf("expected ErrLLMRetriesExhausted, got: %v", err)
	}
}

func TestCallWithRetry_ContextCancel(t *testing.T) {
	// 服务端故意慢响应；取消 ctx 后客户端应立刻返回 context 错误，而不是干等。
	// 处理器加兜底超时，避免 server.Close 因连接未及时释放而阻塞测试。
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer slow.Close()

	a := newTestAgent(t, slow.URL)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := a.callWithRetry(ctx, []any{}, "sys", nil)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !stderrors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("cancellation should return promptly, took %v", elapsed)
	}
}

// ---------- streamWithRetry ----------

func TestStreamWithRetry_SucceedsAfterRetry(t *testing.T) {
	fastRetries(t)
	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n"
	mock := &mockLLM{seq: []mockResp{{429, ""}, {200, sse}}}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	a := newTestAgent(t, srv.URL)
	var got strings.Builder
	resetCalls := 0
	reset := func() { resetCalls++; got.Reset() }
	onMessage := func(sr client.StreamResponse) {
		if len(sr.Choices) > 0 {
			got.WriteString(sr.Choices[0].Delta.Content)
		}
	}

	err := a.streamWithRetry(context.Background(), []any{}, "sys", nil, reset, onMessage)
	if err != nil {
		t.Fatalf("expected stream success after retry, got: %v", err)
	}
	if mock.callCount() != 2 {
		t.Fatalf("expected 2 calls, got %d", mock.callCount())
	}
	if resetCalls != 1 {
		t.Fatalf("expected reset called once before retry, got %d", resetCalls)
	}
	if got.String() != "hi" {
		t.Fatalf("expected accumulated content 'hi', got: %q", got.String())
	}
}

func TestStreamWithRetry_FailsFastOnClientError(t *testing.T) {
	fastRetries(t)
	mock := &mockLLM{seq: []mockResp{{401, ""}}}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	a := newTestAgent(t, srv.URL)
	err := a.streamWithRetry(context.Background(), []any{}, "sys", nil, func() {}, func(client.StreamResponse) {})
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if mock.callCount() != 1 {
		t.Fatalf("401 must not be retried, expected 1 call, got %d", mock.callCount())
	}
}
