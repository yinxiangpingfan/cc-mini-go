package tools

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// Max file size: 1 GiB.
const editMaxFileSize int64 = 1 * 1024 * 1024 * 1024

// EditFileRequest is the input for the file edit tool.
type EditFileRequest struct {
	FilePath   string `json:"file_path" jsonschema:"required,description=Absolute path to file"`
	OldString  string `json:"old_string" jsonschema:"required,description=Exact string to replace"`
	NewString  string `json:"new_string" jsonschema:"required,description=Replacement string"`
	ReplaceAll bool   `json:"replace_all" jsonschema:"description=Replace all occurrences, default false"`
}

// EditFileResult is the output for the file edit tool.
type EditFileResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// editFile executes the file edit logic and returns the result.
func editFile(ctx context.Context, req *EditFileRequest) (*EditFileResult, error) {
	path := req.FilePath

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &EditFileResult{Content: "Error: File not found: " + path, IsError: true}, nil
		}
		return &EditFileResult{Content: "Error: " + err.Error(), IsError: true}, nil
	}
	if info.IsDir() {
		return &EditFileResult{Content: "Error: " + path + " is a directory, not a file", IsError: true}, nil
	}

	if info.Size() > editMaxFileSize {
		return &EditFileResult{Content: "Error: File too large to edit", IsError: true}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return &EditFileResult{Content: "Error reading file: " + err.Error(), IsError: true}, nil
	}

	content := string(data)

	count := strings.Count(content, req.OldString)
	if count == 0 {
		return &EditFileResult{Content: "Error: old_string not found in " + path, IsError: true}, nil
	}
	if count > 1 && !req.ReplaceAll {
		return &EditFileResult{
			Content: "Error: old_string found " + strconv.Itoa(count) + " times. Use replace_all=true or add more context.",
			IsError: true,
		}, nil
	}

	var newContent string
	if req.ReplaceAll {
		newContent = strings.ReplaceAll(content, req.OldString, req.NewString)
	} else {
		newContent = strings.Replace(content, req.OldString, req.NewString, 1)
	}

	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return &EditFileResult{Content: "Error writing file: " + err.Error(), IsError: true}, nil
	}

	replaced := count
	if !req.ReplaceAll {
		replaced = 1
	}
	return &EditFileResult{Content: "Successfully replaced " + strconv.Itoa(replaced) + " occurrence(s) in " + path}, nil
}

// NewEditFileTool creates an InvokableTool for editing files.
func NewEditFileTool() (tool.InvokableTool, error) {
	return utils.InferTool("Edit",
		"Performs exact string replacements in files.\n\n"+
			"Usage:\n"+
			"- You must use your Read tool at least once in the conversation before editing. "+
			"This tool will error if you attempt an edit without reading the file.\n"+
			"- When editing text from Read tool output, ensure you preserve the exact indentation "+
			"(tabs/spaces) as it appears AFTER the line number prefix. The line number prefix format is: "+
			"line number + tab. Everything after that is the actual file content to match. "+
			"Never include any part of the line number prefix in the old_string or new_string.\n"+
			"- ALWAYS prefer editing existing files in the codebase. NEVER write new files unless explicitly required.\n"+
			"- The edit will FAIL if old_string is not unique in the file. Either provide a larger string "+
			"with more surrounding context to make it unique or use replace_all to change every instance of old_string.\n"+
			"- Use replace_all for replacing and renaming strings across the file.",
		editFile)
}
