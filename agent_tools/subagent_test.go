package agent_tools

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/client"
)

// ---------- 测试基础设施：可编程的假 LLM 服务端 ----------

// subagentMockLLM 是一个可编程的 OpenAI 兼容假服务端。
// 按请求顺序依次返回 responses 中的响应体；队列耗尽后，
// 若 repeatLast 为真则重复返回最后一个响应，否则返回 500。
type subagentMockLLM struct {
	mu         sync.Mutex
	responses  []string // 预置的响应体（JSON），按调用顺序消费
	repeatLast bool     // 队列耗尽后是否重复最后一个响应
	status     int      // 非 0 时对所有请求强制返回该状态码
	calls      int      // 已收到的请求数
	bodies     [][]byte // 捕获的请求体，便于断言
}

func (m *subagentMockLLM) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.bodies = append(m.bodies, body)

	if m.status != 0 {
		w.WriteHeader(m.status)
		_, _ = w.Write([]byte(`{}`))
		return
	}

	i := m.calls - 1
	var resp string
	switch {
	case i < len(m.responses):
		resp = m.responses[i]
	case m.repeatLast && len(m.responses) > 0:
		resp = m.responses[len(m.responses)-1]
	default:
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(resp))
}

func (m *subagentMockLLM) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *subagentMockLLM) requestBody(i int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i < 0 || i >= len(m.bodies) {
		return ""
	}
	return string(m.bodies[i])
}

// newSubagentTestRunner 基于指向假服务端的 client 构造一个 SubAgentRunner。
func newSubagentTestRunner(t *testing.T, srvURL string) *SubAgentRunner {
	t.Helper()
	cl, err := client.Init(srvURL, "test-key")
	if err != nil {
		t.Fatalf("client.Init failed: %v", err)
	}
	return &SubAgentRunner{
		call:   client.NewCall(cl, client.NewChatCompletionMessage(), nil),
		model:  "test-model",
		system: "test system prompt",
	}
}

// mustMarshalJSON 把 v 序列化为 JSON 字符串，失败即终止测试。
func mustMarshalJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	return string(b)
}

// buildFinalResp 构造一个「无工具调用、直接给出最终文本」的 ChatCompletion 响应。
func buildFinalResp(t *testing.T, text string) string {
	return mustMarshalJSON(t, map[string]any{
		"choices": []any{
			map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": text,
				},
				"finish_reason": "stop",
			},
		},
	})
}

// buildToolCallResp 构造一个「请求调用某工具」的 ChatCompletion 响应。
func buildToolCallResp(t *testing.T, callID, name, args string) string {
	return mustMarshalJSON(t, map[string]any{
		"choices": []any{
			map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "",
					"tool_calls": []any{
						map[string]any{
							"id":   callID,
							"type": "function",
							"function": map[string]any{
								"name":      name,
								"arguments": args,
							},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
	})
}

// ---------- 参数校验（无需 HTTP） ----------

func TestNewSubAgentTools_MissingPrompt(t *testing.T) {
	tool := NewSubAgentTools(nil, "")
	out := tool.Func(map[string]any{})

	var resp map[string]string
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("output is not valid JSON: %v, raw: %s", err, out)
	}
	if _, ok := resp["error"]; !ok {
		t.Fatalf("expected error when 'prompt' is missing, got: %s", out)
	}
}

func TestNewSubAgentTools_EmptyPrompt(t *testing.T) {
	tool := NewSubAgentTools(nil, "")
	out := tool.Func(map[string]any{"prompt": ""})

	var resp map[string]string
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("output is not valid JSON: %v, raw: %s", err, out)
	}
	if _, ok := resp["error"]; !ok {
		t.Fatalf("expected error when 'prompt' is empty, got: %s", out)
	}
}

func TestNewSubAgentTools_Name(t *testing.T) {
	tool := NewSubAgentTools(nil, "")
	if tool.Name != "task" {
		t.Fatalf("expected tool name 'task', got: %s", tool.Name)
	}
}

// ---------- 工具 schema ----------

func TestSubAgentInfoForLLM_Schema(t *testing.T) {
	var tool *Tools
	info := tool.SubAgentInfoForLLM()

	if info.Type != "function" {
		t.Fatalf("expected type 'function', got: %s", info.Type)
	}
	if info.Function.Name != "task" {
		t.Fatalf("expected function name 'task', got: %s", info.Function.Name)
	}
	if len(info.Function.Parameters.Required) != 1 || info.Function.Parameters.Required[0] != "prompt" {
		t.Fatalf("expected required=['prompt'], got: %v", info.Function.Parameters.Required)
	}
	if _, ok := info.Function.Parameters.Properties["prompt"]; !ok {
		t.Fatalf("expected 'prompt' property in schema, got: %v", info.Function.Parameters.Properties)
	}
}

// ---------- 子工具集（防递归是核心契约） ----------

func TestBuildChildTools_ExcludesTaskTool(t *testing.T) {
	handlers := make(map[string]func(map[string]any) string)
	tools := buildChildTools(&handlers)

	// 核心契约：子 agent 绝不能拿到 task 工具，否则会无限递归派生
	if _, ok := handlers["task"]; ok {
		t.Fatal("child handlers must NOT contain 'task' tool (recursion guard)")
	}
	for _, tl := range tools {
		if tl.Function.Name == "task" {
			t.Fatal("child tool schema must NOT expose 'task' tool (recursion guard)")
		}
	}
}

func TestBuildChildTools_RegistersBaseTools(t *testing.T) {
	handlers := make(map[string]func(map[string]any) string)
	tools := buildChildTools(&handlers)

	want := []string{"time_now", "read_file", "write_file", "Bash", "todo_list"}
	for _, name := range want {
		if _, ok := handlers[name]; !ok {
			t.Errorf("expected child handler %q to be registered", name)
		}
	}

	// schema 列表与 handler map 数量应一致（每个工具一一对应）
	if len(tools) != len(handlers) {
		t.Fatalf("tool schema count (%d) != handler count (%d)", len(tools), len(handlers))
	}
	if len(tools) != len(want) {
		t.Fatalf("expected %d child tools, got: %d", len(want), len(tools))
	}
}

// ---------- run 循环（HTTP 驱动） ----------

func TestSubAgentRun_ReturnsSummary(t *testing.T) {
	mock := &subagentMockLLM{
		responses: []string{buildFinalResp(t, "这是子任务的总结")},
	}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	runner := newSubagentTestRunner(t, srv.URL)
	result := runner.run("分析一下代码")

	if result != "这是子任务的总结" {
		t.Fatalf("expected summary text, got: %q", result)
	}
	if mock.callCount() != 1 {
		t.Fatalf("expected exactly 1 LLM call, got: %d", mock.callCount())
	}
}

func TestSubAgentRun_ToolCallThenSummary(t *testing.T) {
	mock := &subagentMockLLM{
		responses: []string{
			// 第一轮：请求调用安全的 time_now 工具
			buildToolCallResp(t, "call_time_1", "time_now", `{"region":"UTC"}`),
			// 第二轮：给出最终总结
			buildFinalResp(t, "当前 UTC 时间已获取并总结完毕"),
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	runner := newSubagentTestRunner(t, srv.URL)
	result := runner.run("现在 UTC 几点")

	if result != "当前 UTC 时间已获取并总结完毕" {
		t.Fatalf("expected final summary, got: %q", result)
	}
	if mock.callCount() != 2 {
		t.Fatalf("expected 2 LLM calls (tool turn + summary turn), got: %d", mock.callCount())
	}
	// 第二次请求应当把工具结果回传给了模型
	second := mock.requestBody(1)
	if !strings.Contains(second, "call_time_1") {
		t.Fatalf("expected 2nd request to carry tool_call_id 'call_time_1', body: %s", second)
	}
	if !strings.Contains(second, `"role":"tool"`) {
		t.Fatalf("expected 2nd request to carry a tool result message, body: %s", second)
	}
}

func TestSubAgentRun_Non200ReturnsError(t *testing.T) {
	mock := &subagentMockLLM{status: http.StatusInternalServerError}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	runner := newSubagentTestRunner(t, srv.URL)
	result := runner.run("做点什么")

	var resp map[string]string
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("expected JSON error, got non-JSON: %s", result)
	}
	if !strings.Contains(resp["error"], "status code: 500") {
		t.Fatalf("expected status code 500 in error, got: %s", resp["error"])
	}
}

func TestSubAgentRun_EmptyChoicesReturnsNoSummary(t *testing.T) {
	mock := &subagentMockLLM{
		responses: []string{`{"choices":[]}`},
	}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	runner := newSubagentTestRunner(t, srv.URL)
	result := runner.run("空响应测试")

	if result != "(no summary)" {
		t.Fatalf("expected '(no summary)' on empty choices, got: %q", result)
	}
}

func TestSubAgentRun_MaxTurnsSafetyLimit(t *testing.T) {
	// 永远返回工具调用，验证 subAgentMaxTurns 安全上限会终止循环
	mock := &subagentMockLLM{
		responses:  []string{buildToolCallResp(t, "call_loop", "time_now", `{"region":"UTC"}`)},
		repeatLast: true,
	}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	runner := newSubagentTestRunner(t, srv.URL)
	result := runner.run("死循环测试")

	if result != "(no summary)" {
		t.Fatalf("expected '(no summary)' when max turns hit, got: %q", result)
	}
	if mock.callCount() != subAgentMaxTurns {
		t.Fatalf("expected exactly %d LLM calls (max turns), got: %d", subAgentMaxTurns, mock.callCount())
	}
}

func TestSubAgentRun_FirstResponseHasNoTools(t *testing.T) {
	// 第一轮就直接给文本（无任何工具调用），应只发一次请求并直接返回
	mock := &subagentMockLLM{
		responses: []string{buildFinalResp(t, "无需工具，直接回答")},
	}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	runner := newSubagentTestRunner(t, srv.URL)
	result := runner.run("一个简单问题")

	if result != "无需工具，直接回答" {
		t.Fatalf("got: %q", result)
	}
	// 验证首个请求确实携带了用户的任务 prompt
	first := mock.requestBody(0)
	if !strings.Contains(first, "一个简单问题") {
		t.Fatalf("expected 1st request to carry the task prompt, body: %s", first)
	}
}
