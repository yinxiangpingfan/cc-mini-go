package tools

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

const (
	// Default search limit.
	// 默认搜索结果限制。
	defaultHeadLimit = 250

	// Default timeout in seconds.
	// 默认超时时间（秒）。
	defaultSearchTimeout = 30
)

// GrepRequest is the input for the grep tool.
// GrepRequest 是 grep 工具的输入参数。
type GrepRequest struct {
	Pattern         string `json:"pattern" jsonschema:"required,description=Regex pattern to search for"`
	Path            string `json:"path" jsonschema:"description=Directory or file to search, defaults to current directory"`
	Glob            string `json:"glob" jsonschema:"description=File glob filter, e.g. '*.go'"`
	Type            string `json:"type" jsonschema:"description=File type filter, e.g. 'go', 'py', 'rust'"`
	OutputMode      string `json:"output_mode" jsonschema:"description=Output mode: files_with_matches, content, count; defaults to files_with_matches"`
	CaseInsensitive bool   `json:"case_insensitive" jsonschema:"description=Case insensitive search, defaults to false"`
	ShowLineNumbers bool   `json:"show_line_numbers" jsonschema:"description=Show line numbers in content mode, defaults to true"`
	AfterContext    int    `json:"after_context" jsonschema:"description=Lines to show after each match"`
	BeforeContext   int    `json:"before_context" jsonschema:"description=Lines to show before each match"`
	Context         int    `json:"context" jsonschema:"description=Context lines around each match"`
	Multiline       bool   `json:"multiline" jsonschema:"description=Enable multiline matching, defaults to false"`
	HeadLimit       int    `json:"head_limit" jsonschema:"description=Limit output to first N lines, defaults to 250"`
	Offset          int    `json:"offset" jsonschema:"description=Skip first N lines, defaults to 0"`
	Timeout         int    `json:"timeout" jsonschema:"description=Timeout in seconds, defaults to 30"`
}

// GrepResult is the output of the grep tool.
// GrepResult 是 grep 工具的输出结果。
type GrepResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// grepExec executes the grep search and returns matching results.
// grepExec 执行 grep 搜索并返回匹配结果。
func grepExec(ctx context.Context, req *GrepRequest) (*GrepResult, error) {
	path := req.Path
	if path == "" {
		path = "."
	}

	// Resolve the path to absolute path.
	// 解析为绝对路径。
	absPath, err := filepath.Abs(path)
	if err != nil {
		return &GrepResult{
			Content: "Error: Invalid path: " + err.Error(),
			IsError: true,
		}, nil
	}

	// Check if path exists.
	// 检查路径是否存在。
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return &GrepResult{
			Content: "Error: Path not found: " + path,
			IsError: true,
		}, nil
	}

	// Create context with timeout.
	// 创建带超时的上下文。
	searchCtx := ctx
	if req.Timeout <= 0 {
		req.Timeout = defaultSearchTimeout
	}
	var cancel context.CancelFunc
	searchCtx, cancel = context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second)
	defer cancel()

	// Try ripgrep first.
	// 优先使用 ripgrep。
	result, err := grepWithRipgrep(searchCtx, req, absPath)
	if err == nil {
		return result, nil
	}

	// Check if timeout occurred.
	// 检查是否超时。
	if searchCtx.Err() == context.DeadlineExceeded {
		return &GrepResult{
			Content: "Error: Search timed out after " + strconv.Itoa(req.Timeout) + "s",
			IsError: true,
		}, nil
	}

	// Fallback to pure Go implementation.
	// 回退到纯 Go 实现。
	return grepWithGoRegex(searchCtx, req, absPath)
}

// grepWithRipgrep uses ripgrep for fast searching.
// grepWithRipgrep 使用 ripgrep 进行快速搜索。
func grepWithRipgrep(ctx context.Context, req *GrepRequest, path string) (*GrepResult, error) {
	cmd := []string{"rg", "--no-heading"}

	// Case insensitive.
	// 不区分大小写。
	if req.CaseInsensitive {
		cmd = append(cmd, "-i")
	}

	// Multiline mode.
	// 多行模式。
	if req.Multiline {
		cmd = append(cmd, "-U", "--multiline-dotall")
	}

	// Context lines.
	// 上下文行数。
	outputMode := req.OutputMode
	if outputMode == "" {
		outputMode = "files_with_matches"
	}

	if outputMode == "content" {
		if req.AfterContext > 0 {
			cmd = append(cmd, "-A", strconv.Itoa(req.AfterContext))
		}
		if req.BeforeContext > 0 {
			cmd = append(cmd, "-B", strconv.Itoa(req.BeforeContext))
		}
		if req.Context > 0 {
			cmd = append(cmd, "-C", strconv.Itoa(req.Context))
		}
	}

	// Output mode flags.
	// 输出模式标志。
	switch outputMode {
	case "files_with_matches":
		cmd = append(cmd, "-l")
	case "count":
		cmd = append(cmd, "-c")
	case "content":
		if req.ShowLineNumbers {
			cmd = append(cmd, "-n")
		}
	default:
		cmd = append(cmd, "-l")
	}

	// File filters.
	// 文件过滤器。
	if req.Glob != "" {
		cmd = append(cmd, "-g", req.Glob)
	}
	if req.Type != "" {
		cmd = append(cmd, "--type", req.Type)
	}

	cmd = append(cmd, req.Pattern, path)

	// Execute command.
	// 执行命令。
	proc := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	proc.Stderr = nil

	output, err := proc.Output()
	if err != nil {
		// ripgrep not found or error.
		// ripgrep 未找到或出错。
		if _, pathErr := exec.LookPath("rg"); pathErr != nil {
			return nil, pathErr
		}
		return nil, err
	}

	outputStr := strings.TrimSpace(string(output))
	if outputStr == "" {
		return &GrepResult{Content: "No matches found.", IsError: false}, nil
	}

	// Apply offset and head_limit.
	// 应用 offset 和 head_limit。
	result := applyLimits(outputStr, req.Offset, req.HeadLimit)

	return &GrepResult{Content: result, IsError: false}, nil
}

// grepWithGoRegex uses pure Go regex for searching (fallback).
// grepWithGoRegex 使用纯 Go 正则表达式搜索（回退方案）。
func grepWithGoRegex(ctx context.Context, req *GrepRequest, path string) (*GrepResult, error) {
	outputMode := req.OutputMode
	if outputMode == "" {
		outputMode = "files_with_matches"
	}

	headLimit := req.HeadLimit
	if headLimit <= 0 {
		headLimit = defaultHeadLimit
	}

	// Compile regex.
	// 编译正则表达式。
	flags := ""
	if req.CaseInsensitive {
		flags = "(?i)"
	}
	pattern := flags + req.Pattern
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return &GrepResult{
			Content: "Error: Invalid regex pattern: " + err.Error(),
			IsError: true,
		}, nil
	}

	// Get files to search.
	// 获取要搜索的文件。
	files := getFilesToSearch(path, req.Glob)

	var matched []string
	totalLines := 0

	for _, file := range files {
		fileInfo, err := os.Stat(file)
		if err != nil || fileInfo.IsDir() {
			continue
		}

		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(strings.NewReader(string(content)))
		lineNum := 0
		for scanner.Scan() {
			// Check context cancellation.
			// 检查上下文取消。
			select {
			case <-ctx.Done():
				return &GrepResult{
					Content: "Error: Search timed out",
					IsError: true,
				}, ctx.Err()
			default:
			}

			lineNum++
			line := scanner.Text()

			if regex.MatchString(line) {
				totalLines++
				if outputMode == "files_with_matches" {
					if !containsString(matched, file) {
						matched = append(matched, file)
					}
				} else {
					var entry string
					if req.ShowLineNumbers {
						entry = file + ":" + strconv.Itoa(lineNum) + ":" + line
					} else {
						entry = file + ":" + line
					}
					matched = append(matched, entry)
				}
			}
		}
	}

	if len(matched) == 0 {
		return &GrepResult{Content: "No matches found.", IsError: false}, nil
	}

	result := strings.Join(matched, "\n")

	// Apply offset and head_limit.
	// 应用 offset 和 head_limit。
	if req.Offset > 0 || headLimit > 0 {
		lines := strings.Split(result, "\n")
		if req.Offset > 0 && req.Offset < len(lines) {
			lines = lines[req.Offset:]
		}
		if headLimit > 0 && headLimit < len(lines) {
			result = strings.Join(lines[:headLimit], "\n")
			result += "\n\n... (results truncated, showing " + strconv.Itoa(headLimit) + " of " + strconv.Itoa(len(lines)+req.Offset) + " entries)"
		} else if req.Offset > 0 {
			result = strings.Join(lines, "\n")
		}
	}

	return &GrepResult{Content: result, IsError: false}, nil
}

// getFilesToSearch returns a list of files to search.
// getFilesToSearch 返回要搜索的文件列表。
func getFilesToSearch(path, glob string) []string {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}

	if info.IsDir() {
		pattern := glob
		if pattern == "" {
			pattern = "**/*"
		}
		// Use globMatch to support ** patterns.
		// 使用 globMatch 支持 ** 模式。
		matches, err := globMatch(path, pattern)
		if err != nil {
			return nil
		}
		return matches
	}

	return []string{path}
}

// applyLimits applies offset and head_limit to the output.
// applyLimits 对输出应用 offset 和 head_limit 限制。
func applyLimits(output string, offset, headLimit int) string {
	if offset <= 0 && headLimit <= 0 {
		return output
	}

	lines := strings.Split(output, "\n")
	originalLen := len(lines)

	if offset > 0 && offset < len(lines) {
		lines = lines[offset:]
	}

	if headLimit > 0 && headLimit < len(lines) {
		lines = lines[:headLimit]
		result := strings.Join(lines, "\n")
		result += "\n\n... (results truncated, showing " + strconv.Itoa(headLimit) + " of " + strconv.Itoa(originalLen) + " entries)"
		return result
	}

	return strings.Join(lines, "\n")
}

// containsString checks if a slice contains a string.
// containsString 检查切片是否包含字符串。
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// NewGrepTool creates an InvokableTool for text searching.
// NewGrepTool 创建一个可调用的文本搜索工具。
func NewGrepTool() (tool.InvokableTool, error) {
	return utils.InferTool("Grep",
		"A powerful search tool built on ripgrep (rg).\n\n"+
			"Usage:\n"+
			"- Always use Grep for search tasks. Never use Bash grep/rg commands.\n"+
			"- Supports full regex syntax (e.g., 'log.*Error', 'func\\s+\\w+')\n"+
			"- Filter files with glob (e.g., '*.go') or type (e.g., 'go', 'py')\n\n"+
			"Output modes:\n"+
			"- files_with_matches: Only show file paths (default)\n"+
			"- content: Show matching lines with line numbers\n"+
			"- count: Show match counts per file\n\n"+
			"Examples:\n"+
			"- pattern='TODO' glob='*.go' - Find TODO in Go files\n"+
			"- pattern='func main' output_mode='content' - Show lines with 'func main'\n"+
			"- pattern='error' path='src' -i=true - Case-insensitive search in src",
		grepExec)
}
