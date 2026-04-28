package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBash_Simple(t *testing.T) {
	tool, _ := NewBashTool()
	request := BashRequest{
		Command: "echo 'hello world'",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, err := tool.InvokableRun(ctx, string(jsons))
	if err != nil {
		t.Fatalf("Error running tool: %v", err)
	}

	var res BashResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "hello world") {
		t.Errorf("Expected 'hello world', got: %s", res.Content)
	}
}

func TestBash_Stderr(t *testing.T) {
	tool, _ := NewBashTool()
	request := BashRequest{
		Command: "echo 'error' >&2",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res BashResult
	_ = json.Unmarshal([]byte(result), &res)
	if !strings.Contains(res.Content, "[stderr]") {
		t.Errorf("Expected stderr marker, got: %s", res.Content)
	}
}

func TestBash_ExitCode(t *testing.T) {
	tool, _ := NewBashTool()
	request := BashRequest{
		Command: "exit 1",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res BashResult
	_ = json.Unmarshal([]byte(result), &res)
	if !strings.Contains(res.Content, "[exit code: 1]") {
		t.Errorf("Expected exit code 1, got: %s", res.Content)
	}
}

func TestBash_Timeout(t *testing.T) {
	tool, _ := NewBashTool()
	request := BashRequest{
		Command: "python3 -c 'import time; time.sleep(10)'",
		Timeout: 1,
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res BashResult
	_ = json.Unmarshal([]byte(result), &res)
	if !res.IsError {
		t.Errorf("Should return timeout error")
	}
	if !strings.Contains(res.Content, "timed out") {
		t.Errorf("Expected timeout message, got: %s", res.Content)
	}
}

func TestBash_LongOutput(t *testing.T) {
	tool, _ := NewBashTool()
	request := BashRequest{
		Command: "for i in $(seq 1 5000); do echo \"line $i\"; done",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res BashResult
	_ = json.Unmarshal([]byte(result), &res)
	if !strings.Contains(res.Content, "truncated") {
		t.Errorf("Expected truncated output, got: %s", res.Content[:200])
	}
}

func TestBash_Piped(t *testing.T) {
	tool, _ := NewBashTool()
	request := BashRequest{
		Command: "echo 'hello world' | grep 'world'",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res BashResult
	_ = json.Unmarshal([]byte(result), &res)
	if !strings.Contains(res.Content, "hello world") {
		t.Errorf("Expected 'hello world', got: %s", res.Content)
	}
}
