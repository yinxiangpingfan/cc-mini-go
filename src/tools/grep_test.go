package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrep_Simple(t *testing.T) {
	tool, err := NewGrepTool()
	if err != nil {
		t.Fatalf("Failed to create tool: %v", err)
	}

	request := GrepRequest{
		Pattern:    "func main",
		Path:       getProjectRoot() + "/src/tools",
		Glob:       "*.go",
		OutputMode: "files_with_matches",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, err := tool.InvokableRun(ctx, string(jsons))
	if err != nil {
		t.Fatalf("Error running tool: %v", err)
	}

	var res GrepResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
	// Check if any .go file is found with "func main"
	if !strings.Contains(res.Content, ".go") {
		t.Errorf("Expected .go file in result, got: %s", res.Content)
	}
}

func TestGrep_ContentMode(t *testing.T) {
	tool, _ := NewGrepTool()
	request := GrepRequest{
		Pattern:        "func",
		Path:           getProjectRoot() + "/src/tools",
		Glob:           "bash.go",
		OutputMode:     "content",
		ShowLineNumbers: true,
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GrepResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "func") && !strings.Contains(res.Content, "bash.go") {
		t.Errorf("Expected content match, got: %s", res.Content)
	}
}

func TestGrep_NoMatches(t *testing.T) {
	tool, _ := NewGrepTool()
	// Use a pattern that won't exist in any file
	request := GrepRequest{
		Pattern:    `^__THIS_REGEX_CANNOT_MATCH_ANYTHING__$`,
		Path:       getProjectRoot() + "/src/tools",
		OutputMode: "files_with_matches",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GrepResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.Content != "No matches found." {
		t.Errorf("Expected 'No matches found.', got: %s", res.Content)
	}
}

func TestGrep_CaseInsensitive(t *testing.T) {
	tool, _ := NewGrepTool()
	request := GrepRequest{
		Pattern:        "FUNC MAIN",
		Path:           getProjectRoot() + "/src/tools",
		Glob:           "bash.go",
		OutputMode:     "content",
		CaseInsensitive: true,
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GrepResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
}

func TestGrep_PathNotFound(t *testing.T) {
	tool, _ := NewGrepTool()
	request := GrepRequest{
		Pattern: "test",
		Path:    "/nonexistent/path",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GrepResult
	_ = json.Unmarshal([]byte(result), &res)
	if !res.IsError {
		t.Errorf("Should return error for non-existent path")
	}
	if !strings.Contains(res.Content, "not found") {
		t.Errorf("Expected 'not found' error, got: %s", res.Content)
	}
}

func TestGrep_TypeFilter(t *testing.T) {
	tool, _ := NewGrepTool()
	request := GrepRequest{
		Pattern:    "func",
		Path:       getProjectRoot() + "/src/tools",
		Type:       "go",
		OutputMode: "files_with_matches",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GrepResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
}

func TestGrep_HeadLimit(t *testing.T) {
	tool, _ := NewGrepTool()
	request := GrepRequest{
		Pattern:    "func",
		Path:       getProjectRoot() + "/src/tools",
		Glob:       "*_test.go",
		OutputMode: "content",
		HeadLimit:  2,
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GrepResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "truncated") {
		t.Errorf("Expected truncated output, got: %s", res.Content)
	}
}

func TestGrep_CountMode(t *testing.T) {
	tool, _ := NewGrepTool()
	request := GrepRequest{
		Pattern:    "func",
		Path:       getProjectRoot() + "/src/tools",
		Glob:       "bash.go",
		OutputMode: "count",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GrepResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
	if !strings.Contains(res.Content, ":") {
		t.Errorf("Expected count format (file:count), got: %s", res.Content)
	}
}

func TestGrep_FileAsPath(t *testing.T) {
	tool, _ := NewGrepTool()
	request := GrepRequest{
		Pattern:    "func",
		Path:       getProjectRoot() + "/src/tools/bash.go",
		OutputMode: "content",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GrepResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
}

func TestGrep_InvalidRegex(t *testing.T) {
	tool, _ := NewGrepTool()
	request := GrepRequest{
		Pattern:    "[",  // Invalid regex
		Path:       getProjectRoot() + "/src/tools",
		OutputMode: "files_with_matches",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GrepResult
	_ = json.Unmarshal([]byte(result), &res)
	if !res.IsError {
		t.Errorf("Should return error for invalid regex")
	}
}

func TestGrep_ContextLines(t *testing.T) {
	tool, _ := NewGrepTool()
	request := GrepRequest{
		Pattern:        "BashRequest",
		Path:           getProjectRoot() + "/src/tools",
		Glob:           "bash.go",
		OutputMode:     "content",
		Context:        1,
		ShowLineNumbers: true,
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GrepResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
}

func TestGrep_DefaultPath(t *testing.T) {
	tool, _ := NewGrepTool()
	request := GrepRequest{
		Pattern:    "func",
		OutputMode: "files_with_matches",
		// Path is empty, should use current directory.
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GrepResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
}

func TestGrep_Offset(t *testing.T) {
	tool, _ := NewGrepTool()
	request := GrepRequest{
		Pattern:    "func",
		Path:       getProjectRoot() + "/src/tools",
		Glob:       "bash.go",
		OutputMode: "content",
		Offset:     1,
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GrepResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
}

func TestGrep_TempFile(t *testing.T) {
	// Create a temporary file for testing.
	tmpDir := filepath.Join(os.TempDir(), "grep_test")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	tmpFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(tmpFile, []byte("line1: hello world\nline2: hello golang\nline3: goodbye world\n"), 0644)
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	tool, _ := NewGrepTool()
	request := GrepRequest{
		Pattern:        "hello",
		Path:           tmpFile,
		OutputMode:     "content",
		ShowLineNumbers: true,
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GrepResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "hello") {
		t.Errorf("Expected 'hello' in result, got: %s", res.Content)
	}
}

func TestGrep_Timeout(t *testing.T) {
	tool, _ := NewGrepTool()
	request := GrepRequest{
		Pattern: ".",
		Path:    getProjectRoot() + "/src",
		Timeout: 0, // Use default timeout
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GrepResult
	_ = json.Unmarshal([]byte(result), &res)
	// Should not timeout for small directory
	if res.IsError && strings.Contains(res.Content, "timed out") {
		t.Errorf("Should not timeout for small directory")
	}
}
