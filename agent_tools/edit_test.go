package agent_tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/tools"
)

// seedRead 模拟 read_file：把文件当前哈希记录进 ReadFiles，使其可被 edit/write。
func seedRead(t *testing.T, path string) {
	t.Helper()
	hash, err := tools.HashFile(path)
	if err != nil {
		t.Fatalf("hash error: %v", err)
	}
	ReadFiles.MU.Lock()
	ReadFiles.ReadFiles[path] = hash
	ReadFiles.MU.Unlock()
}

func TestEditFile_ReplaceSingle(t *testing.T) {
	resetReadFiles(t)
	tmp := t.TempDir()
	f := filepath.Join(tmp, "a.txt")
	os.WriteFile(f, []byte("hello world\n"), 0644)
	seedRead(t, f)

	n, err := editFile(f, "world", "go", false)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 replacement, got %d", n)
	}
	data, _ := os.ReadFile(f)
	if string(data) != "hello go\n" {
		t.Fatalf("expected 'hello go\\n', got: %q", string(data))
	}
}

func TestEditFile_ReplaceAll(t *testing.T) {
	resetReadFiles(t)
	tmp := t.TempDir()
	f := filepath.Join(tmp, "a.txt")
	os.WriteFile(f, []byte("x x x"), 0644)
	seedRead(t, f)

	n, err := editFile(f, "x", "y", true)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 replacements, got %d", n)
	}
	data, _ := os.ReadFile(f)
	if string(data) != "y y y" {
		t.Fatalf("expected 'y y y', got: %q", string(data))
	}
}

func TestEditFile_NotUniqueWithoutReplaceAll(t *testing.T) {
	resetReadFiles(t)
	tmp := t.TempDir()
	f := filepath.Join(tmp, "a.txt")
	os.WriteFile(f, []byte("dup dup"), 0644)
	seedRead(t, f)

	_, err := editFile(f, "dup", "x", false)
	if err == nil {
		t.Fatal("expected error when old_string is not unique and replace_all is false")
	}
	if !strings.Contains(err.Error(), "not unique") {
		t.Fatalf("expected 'not unique' error, got: %v", err)
	}
	// 文件不应被修改
	data, _ := os.ReadFile(f)
	if string(data) != "dup dup" {
		t.Fatalf("file should be unchanged on failed edit, got: %q", string(data))
	}
}

func TestEditFile_OldStringNotFound(t *testing.T) {
	resetReadFiles(t)
	tmp := t.TempDir()
	f := filepath.Join(tmp, "a.txt")
	os.WriteFile(f, []byte("hello"), 0644)
	seedRead(t, f)

	_, err := editFile(f, "missing", "x", false)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got: %v", err)
	}
}

func TestEditFile_WithoutReadFirst(t *testing.T) {
	resetReadFiles(t)
	tmp := t.TempDir()
	f := filepath.Join(tmp, "a.txt")
	os.WriteFile(f, []byte("hello"), 0644)
	// 没有 seedRead

	_, err := editFile(f, "hello", "hi", false)
	if err == nil || !strings.Contains(err.Error(), "must be read before") {
		t.Fatalf("expected read-before-write error, got: %v", err)
	}
}

func TestEditFile_ModifiedSinceRead(t *testing.T) {
	resetReadFiles(t)
	tmp := t.TempDir()
	f := filepath.Join(tmp, "a.txt")
	os.WriteFile(f, []byte("v1"), 0644)
	seedRead(t, f)
	// 外部修改
	os.WriteFile(f, []byte("v2 external"), 0644)

	_, err := editFile(f, "v2", "x", false)
	if err == nil || !strings.Contains(err.Error(), "modified since last read") {
		t.Fatalf("expected 'modified since last read' error, got: %v", err)
	}
}

func TestEditFile_FileNotExist(t *testing.T) {
	resetReadFiles(t)
	_, err := editFile(filepath.Join(t.TempDir(), "nope.txt"), "a", "b", false)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected 'does not exist' error, got: %v", err)
	}
}

func TestEditFile_UpdatesHashAllowsSecondEdit(t *testing.T) {
	resetReadFiles(t)
	tmp := t.TempDir()
	f := filepath.Join(tmp, "a.txt")
	os.WriteFile(f, []byte("one two"), 0644)
	seedRead(t, f)

	if _, err := editFile(f, "one", "1", false); err != nil {
		t.Fatalf("first edit failed: %v", err)
	}
	// 第二次编辑应成功，因为哈希在第一次编辑后已更新
	if _, err := editFile(f, "two", "2", false); err != nil {
		t.Fatalf("second edit should succeed after hash update, got: %v", err)
	}
	data, _ := os.ReadFile(f)
	if string(data) != "1 2" {
		t.Fatalf("expected '1 2', got: %q", string(data))
	}
}

// ---------- 工具入口（参数校验） ----------

func TestNewEditFileTool_MissingArgs(t *testing.T) {
	tool := NewEditFileTool()
	cases := []map[string]any{
		{},                                     // 缺 file_path
		{"file_path": "/x"},                    // 缺 old_string
		{"file_path": "/x", "old_string": ""},  // old_string 为空
		{"file_path": "/x", "old_string": "a"}, // 缺 new_string
		{"file_path": "/x", "old_string": "a", "new_string": "a"}, // old==new
	}
	for i, args := range cases {
		out := tool.Func(args)
		var resp map[string]any
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			t.Fatalf("case %d: output not JSON: %v", i, err)
		}
		if _, ok := resp["error"]; !ok {
			t.Fatalf("case %d: expected error for args %v, got: %s", i, args, out)
		}
	}
}

func TestNewEditFileTool_Success(t *testing.T) {
	resetReadFiles(t)
	tmp := t.TempDir()
	f := filepath.Join(tmp, "a.txt")
	os.WriteFile(f, []byte("foo bar"), 0644)
	seedRead(t, f)

	out := NewEditFileTool().Func(map[string]any{
		"file_path":  f,
		"old_string": "bar",
		"new_string": "baz",
	})
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	if resp["success"] != true {
		t.Fatalf("expected success, got: %s", out)
	}
	data, _ := os.ReadFile(f)
	if string(data) != "foo baz" {
		t.Fatalf("expected 'foo baz', got: %q", string(data))
	}
}

func TestEditFileInfoForLLm_Schema(t *testing.T) {
	var tool *Tools
	info := tool.EditFileInfoForLLm()
	if info.Function.Name != "edit_file" {
		t.Fatalf("expected name 'edit_file', got: %s", info.Function.Name)
	}
	want := []string{"file_path", "old_string", "new_string"}
	if len(info.Function.Parameters.Required) != len(want) {
		t.Fatalf("expected required %v, got: %v", want, info.Function.Parameters.Required)
	}
}
