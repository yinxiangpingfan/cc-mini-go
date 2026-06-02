package agent_tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type globResult struct {
	Matches   []string `json:"matches"`
	Count     int      `json:"count"`
	Truncated bool     `json:"truncated"`
}

func runGlob(t *testing.T, args map[string]any) globResult {
	t.Helper()
	out := NewGlobTool().Func(context.Background(), args)
	var r globResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("output not JSON: %v, raw: %s", err, out)
	}
	return r
}

func setupGlobTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "main.go"), "x")
	mustWrite(t, filepath.Join(root, "util.go"), "x")
	mustWrite(t, filepath.Join(root, "readme.md"), "x")
	mustWrite(t, filepath.Join(root, "src", "a.go"), "x")
	mustWrite(t, filepath.Join(root, "src", "deep", "b.go"), "x")
	mustWrite(t, filepath.Join(root, "src", "style.css"), "x")
	return root
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestGlob_BasenamePatternMatchesAnyDepth(t *testing.T) {
	root := setupGlobTree(t)
	// "*.go" 不含 /，按文件名匹配，应命中所有层级的 .go
	r := runGlob(t, map[string]any{"pattern": "*.go", "path": root})
	if r.Count != 4 {
		t.Fatalf("expected 4 .go files at any depth, got %d: %v", r.Count, r.Matches)
	}
	for _, want := range []string{"main.go", "util.go", filepath.ToSlash("src/a.go"), filepath.ToSlash("src/deep/b.go")} {
		if !contains(r.Matches, want) {
			t.Fatalf("expected %s in results: %v", want, r.Matches)
		}
	}
}

func TestGlob_DoubleStarPattern(t *testing.T) {
	root := setupGlobTree(t)
	r := runGlob(t, map[string]any{"pattern": "**/*.go", "path": root})
	if r.Count != 4 {
		t.Fatalf("expected 4 .go files for **/*.go, got %d: %v", r.Count, r.Matches)
	}
	// **/*.go 也应匹配根目录下的文件
	if !contains(r.Matches, "main.go") {
		t.Fatalf("**/*.go should match root-level main.go: %v", r.Matches)
	}
}

func TestGlob_ScopedPattern(t *testing.T) {
	root := setupGlobTree(t)
	// src/**/*.go 只命中 src 下的 .go
	r := runGlob(t, map[string]any{"pattern": "src/**/*.go", "path": root})
	if r.Count != 2 {
		t.Fatalf("expected 2 .go files under src, got %d: %v", r.Count, r.Matches)
	}
	if contains(r.Matches, "main.go") {
		t.Fatalf("scoped pattern should not match root main.go: %v", r.Matches)
	}
}

func TestGlob_NoMatches(t *testing.T) {
	root := setupGlobTree(t)
	r := runGlob(t, map[string]any{"pattern": "*.rs", "path": root})
	if r.Count != 0 {
		t.Fatalf("expected 0 matches, got %d: %v", r.Count, r.Matches)
	}
}

func TestGlob_SortedByModTimeDesc(t *testing.T) {
	root := t.TempDir()
	older := filepath.Join(root, "old.go")
	newer := filepath.Join(root, "new.go")
	mustWrite(t, older, "x")
	mustWrite(t, newer, "x")
	now := time.Now()
	os.Chtimes(older, now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	os.Chtimes(newer, now, now)

	r := runGlob(t, map[string]any{"pattern": "*.go", "path": root})
	if len(r.Matches) != 2 {
		t.Fatalf("expected 2 matches, got %v", r.Matches)
	}
	if r.Matches[0] != "new.go" {
		t.Fatalf("expected newest file first, got: %v", r.Matches)
	}
}

func TestGlob_Truncation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < globMaxResults+5; i++ {
		mustWrite(t, filepath.Join(root, fmt.Sprintf("f%03d.go", i)), "x")
	}
	r := runGlob(t, map[string]any{"pattern": "*.go", "path": root})
	if !r.Truncated {
		t.Fatal("expected truncated=true when matches exceed globMaxResults")
	}
	if r.Count != globMaxResults {
		t.Fatalf("expected %d results after truncation, got %d", globMaxResults, r.Count)
	}
}

func TestGlob_SkipsGitDir(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "real.go"), "x")
	mustWrite(t, filepath.Join(root, ".git", "hooks", "thing.go"), "x")
	r := runGlob(t, map[string]any{"pattern": "*.go", "path": root})
	if r.Count != 1 || r.Matches[0] != "real.go" {
		t.Fatalf("expected .git contents skipped, got: %v", r.Matches)
	}
}

func TestGlob_PathNotExist(t *testing.T) {
	out := NewGlobTool().Func(context.Background(), map[string]any{"pattern": "*.go", "path": filepath.Join(t.TempDir(), "nope")})
	var resp map[string]any
	json.Unmarshal([]byte(out), &resp)
	if _, ok := resp["error"]; !ok {
		t.Fatalf("expected error for non-existent path, got: %s", out)
	}
}

func TestGlob_PathIsFile(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "a.go")
	mustWrite(t, f, "x")
	out := NewGlobTool().Func(context.Background(), map[string]any{"pattern": "*.go", "path": f})
	var resp map[string]any
	json.Unmarshal([]byte(out), &resp)
	if _, ok := resp["error"]; !ok {
		t.Fatalf("expected error when path is a file, got: %s", out)
	}
}

func TestGlob_MissingPattern(t *testing.T) {
	out := NewGlobTool().Func(context.Background(), map[string]any{})
	var resp map[string]any
	json.Unmarshal([]byte(out), &resp)
	if _, ok := resp["error"]; !ok {
		t.Fatalf("expected error when pattern missing, got: %s", out)
	}
}

func TestGlobToRegexp_Patterns(t *testing.T) {
	cases := []struct {
		pattern string
		target  string
		want    bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "main.rs", false},
		{"**/*.go", "main.go", true},
		{"**/*.go", "a/b/c.go", true},
		{"src/**/*.ts", "src/x/y.ts", true},
		{"src/**/*.ts", "other/y.ts", false},
		{"?.go", "a.go", true},
		{"?.go", "ab.go", false},
	}
	for _, c := range cases {
		re, err := globToRegexp(c.pattern)
		if err != nil {
			t.Fatalf("compile %q: %v", c.pattern, err)
		}
		if got := re.MatchString(c.target); got != c.want {
			t.Fatalf("glob %q vs %q: got %v want %v", c.pattern, c.target, got, c.want)
		}
	}
}

func TestGlobInfoForLLm_Schema(t *testing.T) {
	var tool *Tools
	info := tool.GlobInfoForLLm()
	if info.Function.Name != "glob" {
		t.Fatalf("expected name 'glob', got: %s", info.Function.Name)
	}
	if len(info.Function.Parameters.Required) != 1 || info.Function.Parameters.Required[0] != "pattern" {
		t.Fatalf("expected required=[pattern], got: %v", info.Function.Parameters.Required)
	}
}
