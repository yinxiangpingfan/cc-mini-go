package agent_tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTempStore 造一个写入临时目录的记忆库，避免污染真实 ~/.cc_mini_go。
func newTempStore(t *testing.T) (*MemoryStore, string) {
	t.Helper()
	dir := t.TempDir()
	return NewMemoryStore(dir), dir
}

// Save 后能 Reload 回来，且正文/type 正确。
func TestMemorySave_RoundTrips(t *testing.T) {
	s, dir := newTempStore(t)
	entry, err := s.Save("prefer-tabs", "User prefers tabs", "user", "The user prefers tabs over spaces.")
	if err != nil {
		t.Fatalf("Save error: %v", err)
	}
	if entry.Path != filepath.Join(dir, "prefer-tabs.md") {
		t.Fatalf("path = %q", entry.Path)
	}

	// 新建一个 store 从磁盘重新加载，验证落盘可被解析
	reloaded := NewMemoryStore(dir)
	got, ok := reloaded.entries["prefer-tabs"]
	if !ok {
		t.Fatal("memory not reloaded from disk")
	}
	if got.Type != "user" {
		t.Fatalf("type = %q, want user", got.Type)
	}
	if got.Body != "The user prefers tabs over spaces." {
		t.Fatalf("body = %q", got.Body)
	}
}

// 非法 type 被拒，且不落盘。
func TestMemorySave_RejectsInvalidType(t *testing.T) {
	s, dir := newTempStore(t)
	_, err := s.Save("x", "d", "bogus", "body")
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "x.md")); !os.IsNotExist(statErr) {
		t.Fatal("invalid-type memory should not be written")
	}
}

// 名字被清洗，路径穿越被挡住。
func TestMemorySave_SanitizesName(t *testing.T) {
	s, dir := newTempStore(t)
	entry, err := s.Save("../../etc/passwd", "d", "project", "body")
	if err != nil {
		t.Fatalf("Save error: %v", err)
	}
	if strings.Contains(entry.Name, "/") || strings.Contains(entry.Name, "..") {
		t.Fatalf("name not sanitized: %q", entry.Name)
	}
	// 文件必须落在目标目录内
	if filepath.Dir(entry.Path) != dir {
		t.Fatalf("escaped write dir: %q", entry.Path)
	}
}

// Describe 按 type 分组、含正文；空库返回空串。
func TestMemoryDescribe(t *testing.T) {
	s, _ := newTempStore(t)
	if s.Describe() != "" {
		t.Fatal("empty store should Describe to empty string")
	}
	_, _ = s.Save("a-pref", "d", "user", "alpha fact")
	_, _ = s.Save("b-conv", "d", "project", "beta fact")
	out := s.Describe()
	if !strings.Contains(out, "[user]") || !strings.Contains(out, "alpha fact") {
		t.Fatalf("describe missing user group/body:\n%s", out)
	}
	if !strings.Contains(out, "[project]") || !strings.Contains(out, "beta fact") {
		t.Fatalf("describe missing project group/body:\n%s", out)
	}
}

// 项目级目录同名覆盖全局级。
func TestMemory_ProjectOverridesGlobal(t *testing.T) {
	global := t.TempDir()
	project := t.TempDir()
	// 先在全局写一条 dup
	gstore := NewMemoryStore(global)
	_, _ = gstore.Save("dup", "d", "user", "from global")
	// 再在项目写同名
	pstore := NewMemoryStore(project)
	_, _ = pstore.Save("dup", "d", "user", "from project")

	// 合并加载：项目在后，应覆盖全局
	merged := NewMemoryStore(global, project)
	if got := merged.entries["dup"].Body; got != "from project" {
		t.Fatalf("project should override global, got %q", got)
	}
}

// Delete 删文件、出表；删不存在的报错。
func TestMemoryDelete(t *testing.T) {
	s, dir := newTempStore(t)
	_, _ = s.Save("gone", "d", "reference", "body")
	if err := s.Delete("gone"); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if _, ok := s.entries["gone"]; ok {
		t.Fatal("entry still present after delete")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "gone.md")); !os.IsNotExist(statErr) {
		t.Fatal("file still on disk after delete")
	}
	if err := s.Delete("never"); err == nil {
		t.Fatal("deleting unknown memory should error")
	}
}

// MEMORY.md 索引随写入重建。
func TestMemoryIndexRebuilt(t *testing.T) {
	s, dir := newTempStore(t)
	_, _ = s.Save("idx-one", "first", "user", "body1")
	raw, err := os.ReadFile(filepath.Join(dir, memoryIndexName))
	if err != nil {
		t.Fatalf("index not written: %v", err)
	}
	if !strings.Contains(string(raw), "idx-one") || !strings.Contains(string(raw), "[user]") {
		t.Fatalf("index missing entry:\n%s", raw)
	}
}

// save_memory 工具：缺参报错、正常写入返回 saved。
func TestSaveMemoryTool(t *testing.T) {
	s, _ := newTempStore(t)
	tool := NewSaveMemoryTool(s)

	if got := tool.Func(context.Background(), map[string]any{"type": "user", "content": "x"}); !strings.Contains(got, "error") {
		t.Fatalf("missing name should error, got %q", got)
	}
	got := tool.Func(context.Background(), map[string]any{
		"name": "via-tool", "type": "user", "content": "remembered via tool",
	})
	if !strings.Contains(got, `"saved":"via-tool"`) {
		t.Fatalf("unexpected tool result: %q", got)
	}
}

// delete_memory 工具：删已存在的返回 deleted。
func TestDeleteMemoryTool(t *testing.T) {
	s, _ := newTempStore(t)
	_, _ = s.Save("drop-me", "d", "user", "body")
	tool := NewDeleteMemoryTool(s)
	got := tool.Func(context.Background(), map[string]any{"name": "drop-me"})
	if !strings.Contains(got, `"deleted":"drop-me"`) {
		t.Fatalf("unexpected delete result: %q", got)
	}
}
