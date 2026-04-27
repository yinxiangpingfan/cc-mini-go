package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// Image extensions that should be base64 encoded.
var imageExtensions = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".bmp":  true,
	".webp": true,
	".svg":  true,
	".ico":  true,
}

// Max file size: 1 GiB.
const maxFileSize int64 = 1 * 1024 * 1024 * 1024

// ReadFileRequest is the input for the file read tool.
type ReadFileRequest struct {
	FilePath string `json:"file_path" jsonschema:"required,description=Absolute path to the file to read"`
	Offset   int    `json:"offset" jsonschema:"description=Line to start from (0-indexed)"`
	Limit    int    `json:"limit" jsonschema:"description=Max lines to return. Default 2000. 0 lines means 2000 lines"`
}

// ReadFileResult is the output for the file read tool.
type ReadFileResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// isBinary checks if a file appears to be binary by looking for null bytes
// in the first 1024 bytes.
func isBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 1024)
	n, err := f.Read(buf)
	if err != nil {
		return false
	}
	return strings.Contains(string(buf[:n]), "\x00") // 是否包含空字节,文本文件通常不包含空字节
}

// mediaTypeForExt returns the MIME type for common image extensions.
// 基于扩展名返回常见的图像MIME类型。
func mediaTypeForExt(ext string) string {
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".svg":
		return "image/svg+xml"
	default:
		return "image/" + strings.TrimPrefix(ext, ".")
	}
}

// readFile executes the file read logic and returns the result.
func readFile(ctx context.Context, req *ReadFileRequest) (*ReadFileResult, error) {
	path := req.FilePath
	offset := req.Offset
	limit := req.Limit
	if limit <= 0 {
		limit = 2000
	}

	info, err := os.Stat(path) // 获取文件信息
	if err != nil {
		if os.IsNotExist(err) {
			return &ReadFileResult{Content: fmt.Sprintf("Error: File not found: %s", path), IsError: true}, nil
		}
		return &ReadFileResult{Content: fmt.Sprintf("Error: %v", err), IsError: true}, nil
	}
	if info.IsDir() {
		return &ReadFileResult{Content: fmt.Sprintf("Error: %s is a directory, not a file", path), IsError: true}, nil
	}

	ext := strings.ToLower(filepath.Ext(path))

	// Handle image files — return base64 encoded content.
	if imageExtensions[ext] {
		data, err := os.ReadFile(path)
		if err != nil {
			return &ReadFileResult{Content: fmt.Sprintf("Error reading image: %v", err), IsError: true}, nil
		}
		b64 := base64.StdEncoding.EncodeToString(data)
		mediaType := mediaTypeForExt(ext)
		return &ReadFileResult{
			Content: fmt.Sprintf("[Image: %s (%d bytes, %s)]\nbase64:%s", path, len(data), mediaType, b64),
		}, nil
	}

	// Binary file detection.
	if isBinary(path) {
		return &ReadFileResult{Content: fmt.Sprintf("Error: %s appears to be a binary file", path), IsError: true}, nil
	}

	// File size check.
	if info.Size() > maxFileSize {
		return &ReadFileResult{Content: fmt.Sprintf("Error: File too large (%d bytes)", info.Size()), IsError: true}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return &ReadFileResult{Content: fmt.Sprintf("Error reading file: %v", err), IsError: true}, nil
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	// Calculate actual slice bounds.
	start := min(offset, len(lines))
	end := min(start+limit, len(lines))

	var sb strings.Builder
	for i := start; i < end; i++ {
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteByte('\t')
		sb.WriteString(lines[i])
		sb.WriteByte('\n')
	}

	if len(lines) > end {
		sb.WriteString(fmt.Sprintf("\n... (%d more lines)", len(lines)-end))
	}

	return &ReadFileResult{Content: sb.String()}, nil
}

// NewReadFileTool creates an InvokableTool for reading files.
func NewReadFileTool() (tool.InvokableTool, error) {
	return utils.InferTool("Read",
		"Reads a file from the local filesystem. You can access any file directly by using this tool. "+
			"Assume this tool is able to read all files on the machine. If the User provides a path to a file assume that path is valid. "+
			"It is okay to read a file that does not exist; an error will be returned.\n\n"+
			"Usage:\n"+
			"- The file_path parameter must be an absolute path, not a relative path\n"+
			"- By default, it reads up to 2000 lines starting from the beginning of the file\n"+
			"- When you already know which part of the file you need, only read that part. This can be important for larger files.\n"+
			"- Results are returned using cat -n format, with line numbers starting at 1\n"+
			"- This tool allows reading images (eg PNG, JPG, etc). When reading an image file the contents are presented visually.\n"+
			"- This tool can only read files, not directories. To read a directory, use an ls command via the Bash tool.\n"+
			"- If you read a file that exists but has empty contents you will receive a system reminder warning in place of file contents.",
		readFile)
}
