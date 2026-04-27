package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setupTestFile(t *testing.T, content string) string {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test_edit.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	return filePath
}

func TestEditFile_Normal(t *testing.T) {
	filePath := setupTestFile(t, "hello world\ngolang is great\nend of file")

	tool, _ := NewEditFileTool()
	request := EditFileRequest{
		FilePath:  filePath,
		OldString: "world",
		NewString: "universe",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, err := tool.InvokableRun(ctx, string(jsons))
	if err != nil {
		t.Fatalf("Error running tool: %v", err)
	}

	var res EditFileResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}

	// Verify content
	data, _ := os.ReadFile(filePath)
	if !stringContains(string(data), "hello universe") {
		t.Errorf("Expected 'hello universe', got: %s", string(data))
	}
}

func TestEditFile_ReplaceAll(t *testing.T) {
	filePath := setupTestFile(t, "foo bar foo baz foo")

	tool, _ := NewEditFileTool()
	request := EditFileRequest{
		FilePath:   filePath,
		OldString:  "foo",
		NewString:  "qux",
		ReplaceAll: true,
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res EditFileResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
	if !stringContains(res.Content, "3 occurrence(s)") {
		t.Errorf("Expected 3 replacements, got: %s", res.Content)
	}

	data, _ := os.ReadFile(filePath)
	if stringContains(string(data), "foo") {
		t.Errorf("Expected no 'foo' left, got: %s", string(data))
	}
}

func TestEditFile_MultipleNotReplaceAll(t *testing.T) {
	filePath := setupTestFile(t, "foo bar foo baz foo")

	tool, _ := NewEditFileTool()
	request := EditFileRequest{
		FilePath:  filePath,
		OldString: "foo",
		NewString: "qux",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res EditFileResult
	_ = json.Unmarshal([]byte(result), &res)
	if !res.IsError {
		t.Errorf("Should return error when multiple matches")
	}
	if !stringContains(res.Content, "found 3 times") {
		t.Errorf("Expected found 3 times error, got: %s", res.Content)
	}
}

func TestEditFile_NotFound(t *testing.T) {
	tool, _ := NewEditFileTool()
	request := EditFileRequest{
		FilePath:  "/tmp/nonexistent_file_12345.txt",
		OldString: "test",
		NewString: "new",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res EditFileResult
	_ = json.Unmarshal([]byte(result), &res)
	if !res.IsError {
		t.Errorf("Not found file should return error")
	}
}

func TestEditFile_Directory(t *testing.T) {
	tool, _ := NewEditFileTool()
	request := EditFileRequest{
		FilePath:  "/Users/easyimpr/Desktop/cc-mini-go",
		OldString: "test",
		NewString: "new",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res EditFileResult
	_ = json.Unmarshal([]byte(result), &res)
	if !res.IsError {
		t.Errorf("Directory should return error")
	}
}

func TestEditFile_NotFoundOldString(t *testing.T) {
	filePath := setupTestFile(t, "hello world")

	tool, _ := NewEditFileTool()
	request := EditFileRequest{
		FilePath:  filePath,
		OldString: "notexist",
		NewString: "new",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res EditFileResult
	_ = json.Unmarshal([]byte(result), &res)
	if !res.IsError {
		t.Errorf("Not found old_string should return error")
	}
}

func TestEditFile_FileNotChanged(t *testing.T) {
	filePath := setupTestFile(t, "hello world")
	originalContent, _ := os.ReadFile(filePath)

	tool, _ := NewEditFileTool()
	request := EditFileRequest{
		FilePath:  filePath,
		OldString: "notexist",
		NewString: "new",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	tool.InvokableRun(ctx, string(jsons))

	// Verify file unchanged
	data, _ := os.ReadFile(filePath)
	if string(data) != string(originalContent) {
		t.Errorf("File should not change when old_string not found")
	}
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
