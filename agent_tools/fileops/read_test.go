package fileops

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadTool_Image：读图片应返回 data URI 的图片结果，供 agent 转成多模态 user 消息。
func TestReadTool_Image(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "pic.png")
	raw := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3}
	if err := os.WriteFile(f, raw, 0644); err != nil {
		t.Fatal(err)
	}

	out := NewReadFile().Func(context.Background(), map[string]any{"file_path": f})
	content, uri, isImage := SplitImageResult(out)
	if !isImage {
		t.Fatalf(".png should be read as image, got: %s", out)
	}
	if !strings.Contains(content, "[Image:") || !strings.Contains(content, "image/png") {
		t.Fatalf("image summary should mention path and media type, got: %s", content)
	}
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
	if uri != want {
		t.Fatalf("data URI mismatch\n got: %s\nwant: %s", uri, want)
	}
}

// TestReadTool_JpgMediaType：.jpg 的媒体类型应规范成 image/jpeg。
func TestReadTool_JpgMediaType(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "photo.jpg")
	if err := os.WriteFile(f, []byte{0xff, 0xd8, 0xff}, 0644); err != nil {
		t.Fatal(err)
	}
	_, uri, isImage := SplitImageResult(NewReadFile().Func(context.Background(), map[string]any{"file_path": f}))
	if !isImage || !strings.HasPrefix(uri, "data:image/jpeg;base64,") {
		t.Fatalf(".jpg media type should be image/jpeg, got data URI: %s", uri)
	}
}

func TestReadFile_OffsetAndLimit(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "lines.txt")
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"
	os.WriteFile(f, []byte(content), 0644)

	// offset=3 limit=2 -> read line 3,4
	resContent, totalLines, startLine, endLine, isDirectory, truncated, isBinary, err := readFile(f, 3, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isDirectory || isBinary {
		t.Fatalf("expected plain file, got dir=%v, binary=%v", isDirectory, isBinary)
	}
	if startLine != 3 {
		t.Fatalf("expected startLine=3, got %d", startLine)
	}
	if endLine != 4 {
		t.Fatalf("expected endLine=4, got %d", endLine)
	}
	if totalLines != 10 {
		t.Fatalf("expected totalLines=10, got %d", totalLines)
	}
	if !truncated {
		t.Fatal("expected truncated=true when limit < total remaining lines")
	}
	if !strings.Contains(resContent, "3 | line3") || !strings.Contains(resContent, "4 | line4") {
		t.Fatalf("expected lines 3 and 4, got: %q", resContent)
	}
	if strings.Contains(resContent, "2 | line2") || strings.Contains(resContent, "5 | line5") {
		t.Fatalf("should not contain line 2 or 5, got: %q", resContent)
	}
}

func TestReadFile_LimitExceedsTotal(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "short.txt")
	os.WriteFile(f, []byte("a\nb\nc\n"), 0644)

	// limit=100 但只有3行，不应截断
	_, totalLines, _, _, _, truncated, _, err := readFile(f, 1, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if totalLines != 3 {
		t.Fatalf("expected totalLines=3, got %d", totalLines)
	}
	if truncated {
		t.Fatal("expected truncated=false when limit > total lines")
	}
}

func TestReadFile_EmptyFile(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "empty.txt")
	os.WriteFile(f, []byte(""), 0644)

	resContent, totalLines, _, _, _, _, isBinary, err := readFile(f, 1, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isBinary {
		t.Fatal("empty file should not be binary")
	}
	if totalLines != 0 {
		t.Fatalf("expected totalLines=0, got %d", totalLines)
	}
	if resContent != "" {
		t.Fatalf("expected empty content, got: %s", resContent)
	}
}
