package tools

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

var readFiles = make(map[string]bool)

func MarkFileRead(filePath string) {
	readFiles[filePath] = true
}

func IsFileRead(filePath string) bool {
	return readFiles[filePath]
}

type WriteFileRequest struct {
	FilePath string `json:"file_path" jsonschema:"required,description=Absolute path to the file to write"`
	Content  string `json:"content" jsonschema:"required,description=The full content to write to the file"`
}

type WriteFileResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

func writeFile(ctx context.Context, req *WriteFileRequest) (*WriteFileResult, error) {
	path := req.FilePath

	// Check if the file exists and if it has been read
	if _, err := os.Stat(path); err == nil {
		// File exists, check if it has been read
		if !IsFileRead(path) {
			return &WriteFileResult{
				Content: "Error: You must read " + path + " before overwriting it. Use the Read tool first.",
				IsError: true,
			}, nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return &WriteFileResult{Content: "Error creating directory: " + err.Error(), IsError: true}, nil
	}

	if err := os.WriteFile(path, []byte(req.Content), 0644); err != nil {
		return &WriteFileResult{Content: "Error writing file: " + err.Error(), IsError: true}, nil
	}

	lines := strings.Count(req.Content, "\n")
	if req.Content != "" && !strings.HasSuffix(req.Content, "\n") {
		lines++
	}

	return &WriteFileResult{Content: "Successfully wrote " + strconv.Itoa(lines) + " lines to " + path}, nil
}

func NewWriteFileTool() (tool.InvokableTool, error) {
	return utils.InferTool("Write",
		"Writes a file to the local filesystem.\n\n"+
			"Usage:\n"+
			"- This tool will overwrite the existing file if there is one at the provided path.\n"+
			"- If this is an existing file, you MUST use the Read tool first to read the file's contents. "+
			"This tool will fail if you did not read the file first.\n"+
			"- Prefer the Edit tool for modifying existing files — it only sends the diff. "+
			"Only use this tool to create new files or for complete rewrites.\n"+
			"- NEVER create documentation files (*.md) or README files unless explicitly requested by the User.\n"+
			"- Only use emojis if the user explicitly requests it. Avoid writing emojis to files unless asked.",
		writeFile)
}
