package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// getProjectRoot returns the project root directory.
func getProjectRoot() string {
	wd, _ := os.Getwd()
	// Remove /src/tools suffix to get project root.
	if strings.HasSuffix(wd, "/src/tools") {
		return wd[:len(wd)-len("/src/tools")]
	}
	return wd
}

func TestGlob_Simple(t *testing.T) {
	tool, err := NewGlobTool()
	if err != nil {
		t.Fatalf("Failed to create tool: %v", err)
	}

	request := GlobRequest{
		Pattern: "*_test.go",
		Path:    getProjectRoot() + "/src/tools",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, err := tool.InvokableRun(ctx, string(jsons))
	if err != nil {
		t.Fatalf("Error running tool: %v", err)
	}

	var res GlobResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "bash_test.go") {
		t.Errorf("Expected bash_test.go in result, got: %s", res.Content)
	}
}

func TestGlob_DoubleStar(t *testing.T) {
	tool, _ := NewGlobTool()
	request := GlobRequest{
		Pattern: "**/*.go",
		Path:    getProjectRoot() + "/src/tools",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GlobResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "bash.go") {
		t.Errorf("Expected bash.go in result, got: %s", res.Content)
	}
}

func TestGlob_NotFound(t *testing.T) {
	tool, _ := NewGlobTool()
	request := GlobRequest{
		Pattern: "nonexistent_*_test.go",
		Path:    getProjectRoot() + "/src/tools",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GlobResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error, got: %s", res.Content)
	}
	if res.Content != "No files found matching the pattern." {
		t.Errorf("Expected 'No files found', got: %s", res.Content)
	}
}

func TestGlob_DirectoryNotFound(t *testing.T) {
	tool, _ := NewGlobTool()
	request := GlobRequest{
		Pattern: "*.go",
		Path:    "/nonexistent/directory",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GlobResult
	_ = json.Unmarshal([]byte(result), &res)
	if !res.IsError {
		t.Errorf("Should return error for non-existent directory")
	}
	if !strings.Contains(res.Content, "Directory not found") {
		t.Errorf("Expected 'Directory not found', got: %s", res.Content)
	}
}

func TestGlob_NotADirectory(t *testing.T) {
	// Create a temporary file to test non-directory path.
	// 创建临时文件来测试非目录路径。
	tmpFile := filepath.Join(os.TempDir(), "glob_test_file.tmp")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile)

	tool, _ := NewGlobTool()
	request := GlobRequest{
		Pattern: "*.go",
		Path:    tmpFile,
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GlobResult
	_ = json.Unmarshal([]byte(result), &res)
	if !res.IsError {
		t.Errorf("Should return error for non-directory path")
	}
	if !strings.Contains(res.Content, "not a directory") {
		t.Errorf("Expected 'not a directory', got: %s", res.Content)
	}
}

func TestGlob_DefaultPath(t *testing.T) {
	tool, _ := NewGlobTool()
	request := GlobRequest{
		Pattern: "*_test.go",
		// Path is empty, should use current directory.
		// Path 为空，应使用当前目录。
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GlobResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
}

func TestGlob_SingleChar(t *testing.T) {
	tool, _ := NewGlobTool()
	request := GlobRequest{
		Pattern: "?.go",
		Path:    getProjectRoot() + "/src/tools",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res GlobResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
}
