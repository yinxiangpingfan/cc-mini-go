package agent_tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFile_NormalFile(t *testing.T) {
	// 创建临时文件
	tmp := t.TempDir()
	f := filepath.Join(tmp, "test.txt")
	os.WriteFile(f, []byte("line1\nline2\nline3\nline4\nline5\n"), 0644)

	content, totalLines, startLine, endLine, isDir, truncated, isBinary, err := readFile(f, 1, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isDir {
		t.Fatal("expected not directory")
	}
	if isBinary {
		t.Fatal("expected not binary")
	}
	if truncated {
		t.Fatal("expected not truncated")
	}
	if totalLines != 5 {
		t.Fatalf("expected totalLines=5, got %d", totalLines)
	}
	if startLine != 1 {
		t.Fatalf("expected startLine=1, got %d", startLine)
	}
	if endLine != 5 {
		t.Fatalf("expected endLine=5, got %d", endLine)
	}
	if !strings.Contains(content, "1 | line1") {
		t.Fatalf("expected content to contain '1 | line1', got: %s", content)
	}
	if !strings.Contains(content, "5 | line5") {
		t.Fatalf("expected content to contain '5 | line5', got: %s", content)
	}
}

func TestReadFile_NoTrailingNewline(t *testing.T) {
	// 测试最后一行没有 \n 的文件（EOF edge case）
	tmp := t.TempDir()
	f := filepath.Join(tmp, "no_newline.txt")
	os.WriteFile(f, []byte("line1\nline2\nline3"), 0644) // 无尾部换行

	content, totalLines, _, _, _, _, _, err := readFile(f, 1, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if totalLines != 3 {
		t.Fatalf("expected totalLines=3, got %d", totalLines)
	}
	if !strings.Contains(content, "3 | line3") {
		t.Fatalf("expected last line 'line3' to be captured, got: %s", content)
	}
}

func TestReadFile_BinaryByExtension(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "image.png")
	os.WriteFile(f, []byte("fake png content"), 0644)

	content, _, _, _, _, _, isBinary, err := readFile(f, 1, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isBinary {
		t.Fatal("expected binary=true for .png file")
	}
	if !strings.Contains(content, "file is binary") {
		t.Fatalf("expected binary message, got: %s", content)
	}
}

func TestReadFile_BinaryByNullByte(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "data.dat")
	// 写入包含 null byte 的内容
	os.WriteFile(f, []byte("hello\x00world"), 0644)

	content, _, _, _, _, _, isBinary, err := readFile(f, 1, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isBinary {
		t.Fatal("expected binary=true for file with null bytes")
	}
	if !strings.Contains(content, "file is binary") {
		t.Fatalf("expected binary message, got: %s", content)
	}
}

func TestReadFile_Directory(t *testing.T) {
	tmp := t.TempDir()

	_, _, _, _, isDir, _, _, err := readFile(tmp, 1, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isDir {
		t.Fatal("expected isDirectory=true")
	}
}

func TestReadFile_NotExist(t *testing.T) {
	_, _, _, _, _, _, _, err := readFile("/tmp/definitely_not_exist_abc123.txt", 1, 50)
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
	if !strings.Contains(err.Error(), "file does not exist") {
		t.Fatalf("expected 'file does not exist' error, got: %v", err)
	}
}

func TestReadFile_OffsetAndLimit(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "lines.txt")
	// 10 行文件
	var sb strings.Builder
	for i := 1; i <= 10; i++ {
		sb.WriteString("line" + strings.Repeat("x", i) + "\n")
	}
	os.WriteFile(f, []byte(sb.String()), 0644)

	// 从第3行开始，取2行
	content, totalLines, startLine, endLine, _, truncated, _, err := readFile(f, 3, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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
	if !strings.Contains(content, "3 | line") {
		t.Fatalf("expected content starting at line 3, got: %s", content)
	}
	// 不应包含第5行
	if strings.Contains(content, "5 | line") {
		t.Fatalf("expected content NOT to contain line 5, got: %s", content)
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

	content, totalLines, _, _, _, _, isBinary, err := readFile(f, 1, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isBinary {
		t.Fatal("empty file should not be binary")
	}
	if totalLines != 0 {
		t.Fatalf("expected totalLines=0, got %d", totalLines)
	}
	if content != "" {
		t.Fatalf("expected empty content, got: %s", content)
	}
}
