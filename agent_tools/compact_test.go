package agent_tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/client"
)

// useTempStorage 把会话存储根指向一个临时目录并在结束时恢复，
// 隔离 PersistLargeOutput / SessionTranscript 写下的文件，避免污染用户 home。
func useTempStorage(t *testing.T) {
	t.Helper()
	old := sessionStorageDir
	sessionStorageDir = t.TempDir()
	t.Cleanup(func() { sessionStorageDir = old })
}

// newCompactTestCall 基于指向假服务端的 client 构造一个 *client.Call。
func newCompactTestCall(t *testing.T, srvURL string) *client.Call {
	t.Helper()
	cl, err := client.Init(srvURL, "test-key")
	if err != nil {
		t.Fatalf("client.Init failed: %v", err)
	}
	return client.NewCall(cl, client.NewChatCompletionMessage(), nil)
}

// ---------- PersistLargeOutput（第 1 层：大结果落盘） ----------

func TestPersistLargeOutput_BelowThresholdUnchanged(t *testing.T) {
	useTempStorage(t)
	out := "short output"
	if got := PersistLargeOutput("call_1", out); got != out {
		t.Fatalf("small output should be returned as-is, got: %q", got)
	}
	if _, err := os.Stat(toolResultsDir()); err == nil {
		t.Fatal("no file should be written for small output")
	}
}

func TestPersistLargeOutput_AboveThresholdPersists(t *testing.T) {
	useTempStorage(t)
	big := strings.Repeat("A", PersistThreshold+1)
	got := PersistLargeOutput("call_big", big)

	if !strings.Contains(got, "<persisted-output>") {
		t.Fatalf("expected persisted-output marker, got: %q", got[:min(80, len(got))])
	}
	if !strings.Contains(got, "Preview:") {
		t.Fatal("expected a Preview section in the marker")
	}
	// 标记里只保留预览，不应包含整份大输出
	if len(got) > PreviewChars+500 {
		t.Fatalf("marker should only carry a preview, but is %d chars", len(got))
	}
	// 全文应已落盘
	stored := filepath.Join(toolResultsDir(), "call_big.txt")
	data, err := os.ReadFile(stored)
	if err != nil {
		t.Fatalf("expected full output persisted to %s: %v", stored, err)
	}
	if len(data) != len(big) {
		t.Fatalf("persisted file should hold full output (%d), got %d", len(big), len(data))
	}
}

// ---------- MicroCompact（第 2 层：旧结果微压缩） ----------

func toolMsg(id, content string) client.ToolsMessage {
	return client.ToolsMessage{Role: "tool", Content: content, ToolsId: id}
}

func TestMicroCompact_KeepsWhenWithinLimit(t *testing.T) {
	longBody := strings.Repeat("x", microCompactMinLen+10)
	msgs := []any{
		client.Message{Role: "user", Content: "hi"},
		toolMsg("t1", longBody),
		toolMsg("t2", longBody),
		toolMsg("t3", longBody),
	}
	out := MicroCompact(msgs)
	for i := 1; i <= 3; i++ {
		if out[i].(client.ToolsMessage).Content != longBody {
			t.Fatalf("with <= %d tool results nothing should be compacted (idx %d)", KeepRecentToolResults, i)
		}
	}
}

func TestMicroCompact_CompactsOlderResults(t *testing.T) {
	longBody := strings.Repeat("x", microCompactMinLen+10)
	msgs := []any{
		toolMsg("t1", longBody), // 最旧 → 应被压缩
		toolMsg("t2", longBody), // 应被压缩
		toolMsg("t3", longBody), // 最近 3 个，保留
		toolMsg("t4", longBody),
		toolMsg("t5", longBody),
	}
	out := MicroCompact(msgs)

	if out[0].(client.ToolsMessage).Content != persistedPlaceholder {
		t.Fatal("oldest tool result should be compacted to placeholder")
	}
	if out[1].(client.ToolsMessage).Content != persistedPlaceholder {
		t.Fatal("second-oldest tool result should be compacted")
	}
	for i := 2; i <= 4; i++ {
		if out[i].(client.ToolsMessage).Content != longBody {
			t.Fatalf("recent tool result idx %d should be kept full", i)
		}
	}
	// ToolsId 必须保留，否则 assistant 的 tool_call 配对会断裂
	if out[0].(client.ToolsMessage).ToolsId != "t1" {
		t.Fatal("compacting must preserve ToolsId for tool_call pairing")
	}
}

func TestMicroCompact_SkipsShortResults(t *testing.T) {
	short := "ok" // < microCompactMinLen，压了也省不了多少
	long := strings.Repeat("y", microCompactMinLen+10)
	msgs := []any{
		toolMsg("t1", short),
		toolMsg("t2", long),
		toolMsg("t3", long),
		toolMsg("t4", long),
	}
	out := MicroCompact(msgs)
	if out[0].(client.ToolsMessage).Content != short {
		t.Fatal("short tool results should not be compacted")
	}
}

func TestMicroCompact_PreservesNonToolMessages(t *testing.T) {
	long := strings.Repeat("z", microCompactMinLen+10)
	user := client.Message{Role: "user", Content: "keep me"}
	msgs := []any{
		user,
		toolMsg("t1", long),
		toolMsg("t2", long),
		toolMsg("t3", long),
		toolMsg("t4", long),
	}
	out := MicroCompact(msgs)
	if out[0].(client.Message).Content != "keep me" {
		t.Fatal("non-tool messages must be preserved untouched")
	}
}

// ---------- EstimateContextSize ----------

func TestEstimateContextSize_GrowsWithContent(t *testing.T) {
	small := []any{client.Message{Role: "user", Content: "hi"}}
	large := []any{client.Message{Role: "user", Content: strings.Repeat("x", 1000)}}
	if EstimateContextSize(small) >= EstimateContextSize(large) {
		t.Fatal("larger message content should yield larger estimated size")
	}
	if EstimateContextSize(small) == 0 {
		t.Fatal("estimate of non-empty messages should be > 0")
	}
}

// ---------- SessionTranscript（持续落盘，与压缩解耦） ----------

// readTranscriptLines 读取转录文件并返回非空行。
func readTranscriptLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("transcript not written: %v", err)
	}
	trimmed := strings.TrimRight(string(data), "\n")
	if trimmed == "" {
		return nil
	}
	lines := strings.Split(trimmed, "\n")
	for _, l := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("each line must be valid JSON: %v (%s)", err, l)
		}
	}
	return lines
}

func TestSessionTranscript_AppendsOnlyNewMessages(t *testing.T) {
	useTempStorage(t)
	tr := NewSessionTranscript()

	// 第一次：两条
	msgs := []any{
		client.Message{Role: "user", Content: "a"},
		client.Message{Role: "assistant", Content: "b"},
	}
	if err := tr.Flush(msgs); err != nil {
		t.Fatalf("flush failed: %v", err)
	}
	if got := readTranscriptLines(t, tr.Path()); len(got) != 2 {
		t.Fatalf("expected 2 lines after first flush, got %d", len(got))
	}

	// 再次 Flush 同样的列表：不应重复写入
	if err := tr.Flush(msgs); err != nil {
		t.Fatalf("flush failed: %v", err)
	}
	if got := readTranscriptLines(t, tr.Path()); len(got) != 2 {
		t.Fatalf("re-flushing same messages should not duplicate, got %d lines", len(got))
	}

	// 追加一条后 Flush：只写增量
	msgs = append(msgs, client.Message{Role: "user", Content: "c"})
	if err := tr.Flush(msgs); err != nil {
		t.Fatalf("flush failed: %v", err)
	}
	if got := readTranscriptLines(t, tr.Path()); len(got) != 3 {
		t.Fatalf("expected 3 lines after appending one, got %d", len(got))
	}
}

func TestSessionTranscript_SurvivesCompaction(t *testing.T) {
	useTempStorage(t)
	tr := NewSessionTranscript()

	// 写入一段较长历史
	msgs := []any{
		client.Message{Role: "user", Content: "1"},
		toolMsg("t1", "result-1"),
		client.Message{Role: "assistant", Content: "2"},
		toolMsg("t2", "result-2"),
	}
	_ = tr.Flush(msgs)
	if got := readTranscriptLines(t, tr.Path()); len(got) != 4 {
		t.Fatalf("expected 4 lines pre-compaction, got %d", len(got))
	}

	// 模拟压缩：内存里塌缩成单条摘要
	compacted := []any{client.Message{Role: "user", Content: "summary"}}
	if err := tr.Flush(compacted); err != nil {
		t.Fatalf("flush after compaction failed: %v", err)
	}

	// 磁盘日志单调增长：4 条原始 + 1 条摘要 = 5，完整历史不被抹掉
	got := readTranscriptLines(t, tr.Path())
	if len(got) != 5 {
		t.Fatalf("compaction must not erase prior on-disk history, expected 5 lines, got %d", len(got))
	}
	if !strings.Contains(got[0], `"1"`) {
		t.Fatalf("first original message must remain on disk, got: %s", got[0])
	}
	if !strings.Contains(got[4], "summary") {
		t.Fatalf("post-compaction summary should be appended, got: %s", got[4])
	}
}

// ---------- DetectManualCompact ----------

func TestDetectManualCompact_Detected(t *testing.T) {
	calls := []client.ToolCall{
		{Function: client.FunctionCall{Name: "read_file", Arguments: `{}`}},
		{Function: client.FunctionCall{Name: CompactToolName, Arguments: `{"focus":"keep the auth refactor"}`}},
	}
	ok, focus := DetectManualCompact(calls)
	if !ok {
		t.Fatal("expected manual compact to be detected")
	}
	if focus != "keep the auth refactor" {
		t.Fatalf("expected focus parsed, got: %q", focus)
	}
}

func TestDetectManualCompact_NotPresent(t *testing.T) {
	calls := []client.ToolCall{
		{Function: client.FunctionCall{Name: "read_file", Arguments: `{}`}},
	}
	if ok, _ := DetectManualCompact(calls); ok {
		t.Fatal("should not detect compact when not requested")
	}
}

// ---------- compact 工具入口与 schema ----------

func TestNewCompactTool_NameAndSentinel(t *testing.T) {
	tool := NewCompactTool()
	if tool.Name != CompactToolName {
		t.Fatalf("expected name %q, got %q", CompactToolName, tool.Name)
	}
	if out := tool.Func(map[string]any{}); !strings.Contains(out, "Compacting") {
		t.Fatalf("expected sentinel output, got: %q", out)
	}
}

func TestCompactInfoForLLM_Schema(t *testing.T) {
	var tool *Tools
	info := tool.CompactInfoForLLM()
	if info.Function.Name != CompactToolName {
		t.Fatalf("expected function name %q, got %q", CompactToolName, info.Function.Name)
	}
	if _, ok := info.Function.Parameters.Properties["focus"]; !ok {
		t.Fatal("expected 'focus' property in schema")
	}
	if len(info.Function.Parameters.Required) != 0 {
		t.Fatalf("focus should be optional, got required: %v", info.Function.Parameters.Required)
	}
}

// ---------- CompactHistory（第 3 层：完整压缩，HTTP 驱动） ----------

func TestCompactHistory_ReplacesWithSummary(t *testing.T) {
	useTempStorage(t)
	mock := &subagentMockLLM{responses: []string{buildFinalResp(t, "已完成 X，修改了 a.go，下一步做 Y")}}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	call := newCompactTestCall(t, srv.URL)
	msgs := []any{
		client.Message{Role: "user", Content: "原始很长的对话"},
		toolMsg("t1", "一些工具结果"),
	}
	state := NewCompactState()
	out := CompactHistory(context.Background(), call, "test-model", msgs, state, "")

	if len(out) != 1 {
		t.Fatalf("compaction should collapse history to a single message, got %d", len(out))
	}
	msg, ok := out[0].(client.Message)
	if !ok || msg.Role != "user" {
		t.Fatalf("compacted message should be a user message, got: %#v", out[0])
	}
	content := msg.Content.(string)
	if !strings.Contains(content, "已完成 X") {
		t.Fatalf("summary text should be carried, got: %q", content)
	}
	if !state.HasCompacted {
		t.Fatal("state.HasCompacted should be true after compaction")
	}
}

func TestCompactHistory_AppendsFocus(t *testing.T) {
	useTempStorage(t)
	mock := &subagentMockLLM{responses: []string{buildFinalResp(t, "摘要正文")}}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	call := newCompactTestCall(t, srv.URL)
	msgs := []any{client.Message{Role: "user", Content: "x"}}
	out := CompactHistory(context.Background(), call, "test-model", msgs, NewCompactState(), "记住要保留登录流程")

	content := out[0].(client.Message).Content.(string)
	if !strings.Contains(content, "记住要保留登录流程") {
		t.Fatalf("focus should be appended to summary, got: %q", content)
	}
}

func TestCompactHistory_FailureKeepsOriginal(t *testing.T) {
	useTempStorage(t)
	// 摘要请求返回 500：压缩应放弃，原样返回消息以保住连续性
	mock := &subagentMockLLM{status: http.StatusInternalServerError}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	call := newCompactTestCall(t, srv.URL)
	msgs := []any{
		client.Message{Role: "user", Content: "a"},
		client.Message{Role: "assistant", Content: "b"},
	}
	out := CompactHistory(context.Background(), call, "test-model", msgs, NewCompactState(), "")

	if len(out) != len(msgs) {
		t.Fatalf("on summary failure the original messages must be kept, got %d want %d", len(out), len(msgs))
	}
}
