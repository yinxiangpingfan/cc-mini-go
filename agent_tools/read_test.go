package agent_tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
		t.Fatalf("jpg should map to image/jpeg, got isImage=%v uri prefix=%.30q", isImage, uri)
	}
}

// TestSplitImageResult_NonImagePassthrough：普通工具结果原样透传，不误判为图片。
func TestSplitImageResult_NonImagePassthrough(t *testing.T) {
	res := `{"content":"hello","total_lines":3}`
	content, uri, isImage := SplitImageResult(res)
	if isImage || uri != "" || content != res {
		t.Fatalf("non-image result must pass through unchanged, got content=%q uri=%q isImage=%v", content, uri, isImage)
	}
}

// TestReadTool_FloatOffsetLimit 回归测试：LLM 传来的 offset/limit 经 JSON 解码是 float64，
// 工具必须正确采纳而非回退默认值（曾因 args["offset"].(int) 断言失败导致参数被忽略）。
func TestReadTool_FloatOffsetLimit(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "lines.txt")
	if err := os.WriteFile(f, []byte("a\nb\nc\nd\ne\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// 模拟真实调用：参数是 float64（json 解码后的数字类型）
	out := NewReadFile().Func(context.Background(), map[string]any{
		"file_path": f,
		"offset":    float64(2),
		"limit":     float64(2),
	})

	var res response
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output not JSON: %v, raw: %s", err, out)
	}
	if res.StartLine != 2 {
		t.Fatalf("offset=2 should start at line 2, got %d (float64 arg ignored?)", res.StartLine)
	}
	if res.EndLine != 3 {
		t.Fatalf("offset=2 limit=2 should end at line 3, got %d", res.EndLine)
	}
	if !strings.Contains(res.Content, "2 | b") || !strings.Contains(res.Content, "3 | c") {
		t.Fatalf("expected lines 2-3 (b,c), got: %s", res.Content)
	}
	if strings.Contains(res.Content, "1 | a") || strings.Contains(res.Content, "4 | d") {
		t.Fatalf("offset/limit not respected, leaked out-of-range lines: %s", res.Content)
	}
	if !res.Truncated {
		t.Fatalf("limit=2 over 5 lines should mark truncated, got false")
	}
}

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
