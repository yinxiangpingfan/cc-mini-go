package agent_tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------- bashTool 核心逻辑 ----------

func TestBashTool_BasicOutput(t *testing.T) {
	out, err := bashTool("echo hello", "", 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello" {
		t.Fatalf("expected 'hello', got: %q", out)
	}
}

func TestBashTool_NoOutput(t *testing.T) {
	out, err := bashTool("true", "", 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "(no output)" {
		t.Fatalf("expected '(no output)', got: %q", out)
	}
}

func TestBashTool_StderrCaptured(t *testing.T) {
	out, err := bashTool("echo err >&2", "", 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "[stderr]") {
		t.Fatalf("expected [stderr] section, got: %q", out)
	}
	if !strings.Contains(out, "err") {
		t.Fatalf("expected stderr content 'err', got: %q", out)
	}
}

func TestBashTool_NonZeroExitCode(t *testing.T) {
	out, err := bashTool("exit 42", "", 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "[exit code: 42]") {
		t.Fatalf("expected exit code in output, got: %q", out)
	}
}

func TestBashTool_StdoutAndStderr(t *testing.T) {
	out, err := bashTool("echo out; echo err >&2", "", 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "out") {
		t.Fatalf("expected stdout 'out', got: %q", out)
	}
	if !strings.Contains(out, "[stderr]") {
		t.Fatalf("expected [stderr], got: %q", out)
	}
}

func TestBashTool_TrailingNewlineTrimmed(t *testing.T) {
	out, err := bashTool("printf 'line1\\nline2\\n'", "", 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.HasSuffix(out, "\n") {
		t.Fatalf("expected trailing newline trimmed, got: %q", out)
	}
	if !strings.Contains(out, "line1") || !strings.Contains(out, "line2") {
		t.Fatalf("expected both lines, got: %q", out)
	}
}

func TestBashTool_Timeout(t *testing.T) {
	_, err := bashTool("sleep 10", "", 200*time.Millisecond, false)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected 'timed out' in error, got: %v", err)
	}
}

func TestBashTool_OutputTruncated(t *testing.T) {
	// 生成 >10000 字符的输出，用 python3 或 awk
	cmd := `python3 -c "print('x' * 20000)"`
	out, err := bashTool(cmd, "", 0, false)
	if err != nil {
		t.Skipf("python3 not available, skipping truncation test: %v", err)
	}
	if !strings.Contains(out, "output truncated") {
		t.Fatalf("expected truncation message, got output of len %d", len(out))
	}
	if !strings.Contains(out, "20000 chars") {
		t.Fatalf("expected original char count in message, got: %q", out)
	}
}

func TestBashTool_WorksInTempDir(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "hello.txt")
	os.WriteFile(f, []byte("world"), 0644)

	out, err := bashTool("cat "+f, "", 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "world" {
		t.Fatalf("expected 'world', got: %q", out)
	}
}

func TestBashTool_MultilineOutput(t *testing.T) {
	out, err := bashTool("printf 'a\\nb\\nc'", "", 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), out)
	}
}

// ---------- NewBashTool 参数解析 ----------

func TestNewBashTool_MissingCommand(t *testing.T) {
	tool := NewBashTool()
	result := tool.Func(map[string]interface{}{})
	var m map[string]string
	if err := json.Unmarshal([]byte(result), &m); err != nil {
		t.Fatalf("invalid JSON: %v, got: %s", err, result)
	}
	if m["error"] == "" {
		t.Fatal("expected error field for missing command")
	}
}

func TestNewBashTool_Success(t *testing.T) {
	tool := NewBashTool()
	result := tool.Func(map[string]interface{}{
		"command": "echo hi",
	})
	var m map[string]string
	if err := json.Unmarshal([]byte(result), &m); err != nil {
		t.Fatalf("invalid JSON: %v, got: %s", err, result)
	}
	if m["output"] != "hi" {
		t.Fatalf("expected output='hi', got: %q", m["output"])
	}
}

func TestNewBashTool_TimeoutParam(t *testing.T) {
	tool := NewBashTool()
	result := tool.Func(map[string]interface{}{
		"command": "sleep 10",
		"timeout": float64(0.2), // 200ms
	})
	var m map[string]string
	if err := json.Unmarshal([]byte(result), &m); err != nil {
		t.Fatalf("invalid JSON: %v, got: %s", err, result)
	}
	if m["error"] == "" {
		t.Fatal("expected timeout error")
	}
}

func TestNewBashTool_OptionalDescriptionDefaults(t *testing.T) {
	tool := NewBashTool()
	// description 不传，不应 panic 或报错
	result := tool.Func(map[string]interface{}{
		"command": "echo ok",
	})
	if !strings.Contains(result, "ok") {
		t.Fatalf("expected output with 'ok', got: %s", result)
	}
}

func TestNewBashTool_OutputIsValidJSON(t *testing.T) {
	tool := NewBashTool()
	// 输出含特殊字符：引号、反斜杠
	result := tool.Func(map[string]interface{}{
		"command": `echo 'say "hello\nworld"'`,
	})
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(result), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\ngot: %s", err, result)
	}
}
