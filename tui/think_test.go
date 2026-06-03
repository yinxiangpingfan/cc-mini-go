package main

import "testing"

// newParseModel 造一个仅够测试流式解析的 model（定位下标初始为「需新开块」）。
func newParseModel() *model {
	return &model{curAsstIdx: -1, curThinkIdx: -1}
}

// textByKind 把某类条目的文本按出现顺序拼接，便于断言。
func (m *model) textByKind(k entryKind) string {
	s := ""
	for _, e := range m.entries {
		if e.kind == k {
			s += e.text
		}
	}
	return s
}

// 单个分块内含完整 <think>…</think>：思考段进思考块，其余进正文。
func TestFeedContent_InlineThink(t *testing.T) {
	m := newParseModel()
	m.feedContent("hello <think>secret</think> world")

	if got := m.textByKind(entryThinking); got != "secret" {
		t.Fatalf("thinking = %q, want %q", got, "secret")
	}
	if got := m.textByKind(entryAssistant); got != "hello  world" {
		t.Fatalf("assistant = %q, want %q", got, "hello  world")
	}
}

// 标签被切到不同分块（<thi|nk> 与 </thin|k>）：仍能正确归类。
func TestFeedContent_SplitAcrossChunks(t *testing.T) {
	m := newParseModel()
	for _, c := range []string{"ab<thi", "nk>xy", "</thin", "k>cd"} {
		m.feedContent(c)
	}
	if got := m.textByKind(entryThinking); got != "xy" {
		t.Fatalf("thinking = %q, want %q", got, "xy")
	}
	if got := m.textByKind(entryAssistant); got != "abcd" {
		t.Fatalf("assistant = %q, want %q", got, "abcd")
	}
}

// 不含标签的普通文本原样进正文，且不会把 "<" 误吞。
func TestFeedContent_PlainAndAngleBracket(t *testing.T) {
	m := newParseModel()
	m.feedContent("a <b> c") // "<b>" 不是 think 标签
	// 残留缓冲可能留住末尾的半截 "<"，flush 后应完整
	m.emitContentSeg(m.thinkPending)
	m.thinkPending = ""
	if got := m.textByKind(entryAssistant); got != "a <b> c" {
		t.Fatalf("assistant = %q, want %q", got, "a <b> c")
	}
	if got := m.textByKind(entryThinking); got != "" {
		t.Fatalf("thinking should be empty, got %q", got)
	}
}

// suffixPrefixLen：识别尾部的半截标签长度。
func TestSuffixPrefixLen(t *testing.T) {
	cases := []struct {
		s, tag string
		want   int
	}{
		{"ab<thi", "<think>", 4},
		{"</thin", "</think>", 6},
		{"hello", "<think>", 0},
		{"x<", "<think>", 1},
	}
	for _, c := range cases {
		if got := suffixPrefixLen(c.s, c.tag); got != c.want {
			t.Fatalf("suffixPrefixLen(%q,%q) = %d, want %d", c.s, c.tag, got, c.want)
		}
	}
}

// updatePlan 解析 todo_list 结果；错误结果不覆盖现有计划。
func TestUpdatePlan(t *testing.T) {
	m := newParseModel()
	m.updatePlan(`{"todos":[{"subject":"A","status":"completed"},{"subject":"B","status":"in_progress"}]}`)
	if len(m.plan) != 2 {
		t.Fatalf("plan len = %d, want 2", len(m.plan))
	}
	if !m.hasActivePlan() {
		t.Fatal("plan with in_progress item should be active")
	}

	m.updatePlan(`{"error":"boom"}`) // 不应清空
	if len(m.plan) != 2 {
		t.Fatalf("error result should not clobber plan, len = %d", len(m.plan))
	}
}

// 全部完成时面板收起（hasActivePlan 为 false）。
func TestHasActivePlan_AllCompleted(t *testing.T) {
	m := newParseModel()
	m.updatePlan(`{"todos":[{"subject":"A","status":"completed"}]}`)
	if m.hasActivePlan() {
		t.Fatal("all-completed plan should collapse the panel")
	}
}
