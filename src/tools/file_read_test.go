package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileRead_Normal(t *testing.T) {
	tool, err := NewReadFileTool()
	if err != nil {
		t.Fatalf("Failed to create tool: %v", err)
	}

	request := ReadFileRequest{
		FilePath: "/Users/easyimpr/Desktop/cc-mini-go/src/tools/test/data/test.c",
		Offset:   0,
		Limit:    100,
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, err := tool.InvokableRun(ctx, string(jsons))
	if err != nil {
		t.Errorf("Error running tool: %v", err)
	}

	var res ReadFileResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "1\t") {
		t.Errorf("Expected line number prefix, got: %s", res.Content[:min(50, len(res.Content))])
	}
}

func TestFileRead_Binary(t *testing.T) {
	tool, _ := NewReadFileTool()
	binaryPath := "/Users/easyimpr/Desktop/cc-mini-go/src/tools/test/data/test_binary"

	// Ensure binary exists
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Binary file not compiled yet")
	}

	request := ReadFileRequest{
		FilePath: binaryPath,
		Offset:   0,
		Limit:    0,
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res ReadFileResult
	_ = json.Unmarshal([]byte(result), &res)
	if !res.IsError {
		t.Errorf("Binary file should return error")
	}
	if !strings.Contains(res.Content, "binary") {
		t.Errorf("Expected binary error message, got: %s", res.Content)
	}
}

func TestFileRead_NotFound(t *testing.T) {
	tool, _ := NewReadFileTool()
	request := ReadFileRequest{
		FilePath: "/tmp/nonexistent_file_12345.txt",
		Offset:   0,
		Limit:    0,
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res ReadFileResult
	_ = json.Unmarshal([]byte(result), &res)
	if !res.IsError {
		t.Errorf("Not found file should return error")
	}
}

func TestFileRead_Directory(t *testing.T) {
	tool, _ := NewReadFileTool()
	request := ReadFileRequest{
		FilePath: "/Users/easyimpr/Desktop/cc-mini-go",
		Offset:   0,
		Limit:    0,
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res ReadFileResult
	_ = json.Unmarshal([]byte(result), &res)
	if !res.IsError {
		t.Errorf("Directory should return error")
	}
}

func TestFileRead_WithOffset(t *testing.T) {
	tool, _ := NewReadFileTool()
	request := ReadFileRequest{
		FilePath: "/Users/easyimpr/Desktop/cc-mini-go/src/tools/test/data/test.c",
		Offset:   5,
		Limit:    3,
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res ReadFileResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Should not be error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "6\t") {
		t.Errorf("Expected line starting at 6, got: %s", res.Content)
	}
}

func TestFileRead_WithImage(t *testing.T) {
	tool, _ := NewReadFileTool()

	// Check if there's an image in test data
	testDir := "/Users/easyimpr/Desktop/cc-mini-go/src/tools/test/data"
	entries, _ := os.ReadDir(testDir)
	var imgPath string
	for _, e := range entries {
		if !e.IsDir() {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" {
				imgPath = filepath.Join(testDir, e.Name())
				break
			}
		}
	}

	if imgPath == "" {
		t.Skip("No image file found in test data")
	}

	request := ReadFileRequest{
		FilePath: imgPath,
		Offset:   0,
		Limit:    0,
	}
	jsons, _ := json.Marshal(request)
	ctx := context.Background()

	result, _ := tool.InvokableRun(ctx, string(jsons))

	var res ReadFileResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.IsError {
		t.Errorf("Image should not be error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "[Image:") || !strings.Contains(res.Content, "base64:") {
		t.Errorf("Expected image format with base64, got: %s", res.Content[:min(100, len(res.Content))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
