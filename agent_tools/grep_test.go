package agent_tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// grepResult 解析 grep 工具返回的 JSON
type grepResult struct {
	Mode      string   `json:"mode"`
	Matches   []string `json:"matches"`
	Count     int      `json:"count"`
	Truncated bool     `json:"truncated"`
}

// setupGrepTree 在临时目录里铺一组测试文件，返回根目录
func setupGrepTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.go"), "package main\nfunc Foo() {}\n// TODO: fix\n")
	mustWrite(t, filepath.Join(root, "b.go"), "package main\nfunc Bar() {}\n")
	mustWrite(t, filepath.Join(root, "notes.txt"), "todo here\nFOO bar\n")
	mustWrite(t, filepath.Join(root, "sub", "c.go"), "package sub\nfunc Foo() {}\n")
	return root
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func runGrep(t *testing.T, args map[string]any) grepResult {
	t.Helper()
	out := NewGrepTool().Func(context.Background(), args)
	var r grepResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("output not JSON: %v, raw: %s", err, out)
	}
	return r
}

func TestGrep_FilesWithMatches(t *testing.T) {
	root := setupGrepTree(t)
	r := runGrep(t, map[string]any{"pattern": "func Foo", "path": root})
	if r.Mode != "files_with_matches" {
		t.Fatalf("expected default mode files_with_matches, got %s", r.Mode)
	}
	// a.go 和 sub/c.go 含有 "func Foo"
	if r.Count != 2 {
		t.Fatalf("expected 2 files, got %d: %v", r.Count, r.Matches)
	}
}

func TestGrep_GlobFilter(t *testing.T) {
	root := setupGrepTree(t)
	// 只在 .go 文件里搜 "func"，notes.txt 应被排除
	r := runGrep(t, map[string]any{"pattern": "func", "path": root, "glob": "*.go"})
	for _, m := range r.Matches {
		if filepath.Ext(m) != ".go" {
			t.Fatalf("glob filter leaked non-go file: %s", m)
		}
	}
	if r.Count != 3 {
		t.Fatalf("expected 3 go files with 'func', got %d: %v", r.Count, r.Matches)
	}
}

func TestGrep_ContentModeWithLineNumbers(t *testing.T) {
	root := setupGrepTree(t)
	r := runGrep(t, map[string]any{"pattern": "TODO", "path": root, "output_mode": "content"})
	if r.Count != 1 {
		t.Fatalf("expected 1 content match, got %d: %v", r.Count, r.Matches)
	}
	// 形如 path:3:// TODO: fix
	if !strings.Contains(r.Matches[0], ":3:") {
		t.Fatalf("expected line number 3 in content output, got: %s", r.Matches[0])
	}
}

func TestGrep_CaseInsensitive(t *testing.T) {
	root := setupGrepTree(t)
	// "foo" 区分大小写时只命中小写出现处；-i 时还应命中 FOO
	insensitive := runGrep(t, map[string]any{"pattern": "foo", "path": root, "output_mode": "count", "-i": true})
	if insensitive.Count == 0 {
		t.Fatal("expected case-insensitive matches for 'foo'")
	}
	sensitive := runGrep(t, map[string]any{"pattern": "FOO", "path": root, "output_mode": "files_with_matches"})
	// 只有 notes.txt 含大写 FOO
	if sensitive.Count != 1 {
		t.Fatalf("expected 1 file with uppercase FOO, got %d: %v", sensitive.Count, sensitive.Matches)
	}
}

func TestGrep_CountMode(t *testing.T) {
	root := setupGrepTree(t)
	r := runGrep(t, map[string]any{"pattern": "package", "path": root, "output_mode": "count"})
	// 只有 3 个 .go 文件含 "package"（notes.txt 不含）
	if r.Count != 3 {
		t.Fatalf("expected 3 files with counts, got %d: %v", r.Count, r.Matches)
	}
	for _, m := range r.Matches {
		if !strings.HasSuffix(m, ":1") {
			t.Fatalf("expected count :1 per file, got: %s", m)
		}
	}
}

func TestGrep_NoMatches(t *testing.T) {
	root := setupGrepTree(t)
	r := runGrep(t, map[string]any{"pattern": "zzz_nonexistent", "path": root})
	if r.Count != 0 || len(r.Matches) != 0 {
		t.Fatalf("expected no matches, got %d: %v", r.Count, r.Matches)
	}
}

func TestGrep_HeadLimitTruncates(t *testing.T) {
	root := setupGrepTree(t)
	r := runGrep(t, map[string]any{"pattern": "package", "path": root, "head_limit": float64(2)})
	if !r.Truncated {
		t.Fatal("expected truncated=true when results exceed head_limit")
	}
	if r.Count != 2 {
		t.Fatalf("expected exactly 2 results at head_limit, got %d", r.Count)
	}
}

func TestGrep_SingleFilePath(t *testing.T) {
	root := setupGrepTree(t)
	r := runGrep(t, map[string]any{"pattern": "Foo", "path": filepath.Join(root, "a.go"), "output_mode": "content"})
	if r.Count != 1 {
		t.Fatalf("expected 1 match in single file, got %d: %v", r.Count, r.Matches)
	}
}

func TestGrep_InvalidRegex(t *testing.T) {
	root := setupGrepTree(t)
	out := NewGrepTool().Func(context.Background(), map[string]any{"pattern": "func(", "path": root})
	var resp map[string]any
	json.Unmarshal([]byte(out), &resp)
	if _, ok := resp["error"]; !ok {
		t.Fatalf("expected error for invalid regex, got: %s", out)
	}
}

func TestGrep_MissingPattern(t *testing.T) {
	out := NewGrepTool().Func(context.Background(), map[string]any{})
	var resp map[string]any
	json.Unmarshal([]byte(out), &resp)
	if _, ok := resp["error"]; !ok {
		t.Fatalf("expected error when pattern missing, got: %s", out)
	}
}

func TestGrep_InvalidOutputMode(t *testing.T) {
	out := NewGrepTool().Func(context.Background(), map[string]any{"pattern": "x", "output_mode": "bogus"})
	var resp map[string]any
	json.Unmarshal([]byte(out), &resp)
	if _, ok := resp["error"]; !ok {
		t.Fatalf("expected error for invalid output_mode, got: %s", out)
	}
}

func TestGrep_Multiline(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "m.go"), "type T struct {\n\tField int\n}\n")
	// 跨行匹配 struct { ... Field
	r := runGrep(t, map[string]any{
		"pattern":     `struct \{[\s\S]*?Field`,
		"path":        root,
		"output_mode": "files_with_matches",
		"multiline":   true,
	})
	if r.Count != 1 {
		t.Fatalf("expected 1 multiline match, got %d: %v", r.Count, r.Matches)
	}
}
