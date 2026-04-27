package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFile_NewFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "new_file.txt")

	MarkFileRead(filePath) // 模拟 Read 过的文件

	tool, _ := NewWriteFileTool()
	request := WriteFileRequest{
		FilePath: filePath,
		Content:  "hello world\nsecond line",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, err := tool.InvokableRun(ctx, string(jsons))
	if err != nil {
		t.Fatalf("Error running tool: %v", err)
	}

	var res WriteFileResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}

	data, _ := os.ReadFile(filePath)
	if string(data) != "hello world\nsecond line" {
		t.Errorf("Expected content, got: %s", string(data))
	}
}

func TestWriteFile_ExistingFileNotRead(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "existing.txt")
	os.WriteFile(filePath, []byte("original"), 0644)

	tool, _ := NewWriteFileTool()
	request := WriteFileRequest{
		FilePath: filePath,
		Content:  "new content",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res WriteFileResult
	_ = json.Unmarshal([]byte(result), &res)
	if !res.IsError {
		t.Errorf("Should return error when file exists but not read")
	}
}

func TestWriteFile_ExistingFileRead(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "existing.txt")
	os.WriteFile(filePath, []byte("original"), 0644)

	MarkFileRead(filePath) // 先标记已读

	tool, _ := NewWriteFileTool()
	request := WriteFileRequest{
		FilePath: filePath,
		Content:  "new content",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res WriteFileResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}

	data, _ := os.ReadFile(filePath)
	if string(data) != "new content" {
		t.Errorf("Expected overwritten content")
	}
}

func TestWriteFile_CreateParentDir(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "subdir1", "subdir2", "file.txt")

	tool, _ := NewWriteFileTool()
	request := WriteFileRequest{
		FilePath: filePath,
		Content:  "nested content",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res WriteFileResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Errorf("File should be created")
	}
}

func TestWriteFile_EmptyContent(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "empty.txt")

	tool, _ := NewWriteFileTool()
	request := WriteFileRequest{
		FilePath: filePath,
		Content:  "",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res WriteFileResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
	if !contains(res.Content, "0 lines") {
		t.Errorf("Expected 0 lines, got: %s", res.Content)
	}
}

func TestWriteFile_LineCount(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "lines.txt")

	tool, _ := NewWriteFileTool()
	request := WriteFileRequest{
		FilePath: filePath,
		Content:  "line1\nline2\nline3",
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res WriteFileResult
	_ = json.Unmarshal([]byte(result), &res)
	if !contains(res.Content, "3 lines") {
		t.Errorf("Expected 3 lines, got: %s", res.Content)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
