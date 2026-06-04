package main

import (
	"strings"
	"testing"
)

func TestSplitFlushable(t *testing.T) {
	if h, tl := splitFlushable("para1\n\npara2 typing"); h != "para1" || tl != "para2 typing" {
		t.Fatalf("para case: head=%q tail=%q", h, tl)
	}
	if h, tl := splitFlushable("one long line still typing"); h != "" || tl != "one long line still typing" {
		t.Fatalf("no-boundary: head=%q tail=%q", h, tl)
	}
	// 未闭合代码块：intro 段刷出，未闭合 fence 整体留下
	if h, tl := splitFlushable("intro\n\n```go\ncode\n"); h != "intro" || !strings.HasPrefix(tl, "```go") {
		t.Fatalf("open fence: head=%q tail=%q", h, tl)
	}
	// 闭合代码块后接空行：整块可刷
	if h, tl := splitFlushable("```go\ncode\n```\n\nnext"); !strings.Contains(h, "```") || tl != "next" {
		t.Fatalf("closed fence: head=%q tail=%q", h, tl)
	}
}

// seal：刷出已完成的队首条目 + 活动助手块的完整段落，只留正在写的尾巴。
func TestSeal_FlushesCompletedPrefix(t *testing.T) {
	m := &model{width: 80, curAsstIdx: 1, curThinkIdx: -1, toolIdx: map[string]int{}}
	m.entries = []entry{
		{kind: entryUser, text: "hi"},
		{kind: entryAssistant, text: "done para\n\nstill typing"},
	}
	if cmd := m.seal(); cmd == nil {
		t.Fatal("expected a flush command")
	}
	if len(m.entries) != 1 || m.entries[0].kind != entryAssistant || m.entries[0].text != "still typing" {
		t.Fatalf("remaining entries = %+v", m.entries)
	}
	if m.curAsstIdx != 0 || !m.asstMarkerShown {
		t.Fatalf("curAsstIdx=%d asstMarkerShown=%v", m.curAsstIdx, m.asstMarkerShown)
	}
}

// seal：遇到运行中的工具就停（保序），并正确维护 toolIdx。
func TestSeal_StopsAtRunningTool(t *testing.T) {
	m := &model{width: 80, curAsstIdx: -1, curThinkIdx: -1,
		toolIdx: map[string]int{"t1": 1, "t2": 2}}
	m.entries = []entry{
		{kind: entryUser, text: "hi"},
		{kind: entryTool, toolName: "bash", toolDone: true, text: `{"ok":1}`},
		{kind: entryTool, toolName: "grep", toolDone: false},
	}
	if cmd := m.seal(); cmd == nil {
		t.Fatal("expected user + done-tool to flush")
	}
	if len(m.entries) != 1 || m.entries[0].toolName != "grep" {
		t.Fatalf("remaining = %+v", m.entries)
	}
	if _, ok := m.toolIdx["t1"]; ok {
		t.Fatal("t1 (flushed) should be removed from toolIdx")
	}
	if m.toolIdx["t2"] != 0 {
		t.Fatalf("t2 idx = %d, want 0", m.toolIdx["t2"])
	}
}

// seal：活动助手块尚无完整段落时不刷。
func TestSeal_KeepsIncompleteActive(t *testing.T) {
	m := &model{width: 80, curAsstIdx: 0, curThinkIdx: -1, toolIdx: map[string]int{}}
	m.entries = []entry{{kind: entryAssistant, text: "one line, no blank yet"}}
	if cmd := m.seal(); cmd != nil {
		t.Fatal("nothing should flush yet")
	}
	if len(m.entries) != 1 || m.entries[0].text != "one line, no blank yet" {
		t.Fatalf("entry changed: %+v", m.entries)
	}
}
