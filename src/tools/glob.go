package tools

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

const (
	// Max results to return before truncation.
	// 返回截断前的最大结果数。
	maxResults = 100

	// Default search directory.
	// 默认搜索目录。
	defaultPath = "."
)

// GlobRequest is the input for the glob tool.
// GlobRequest 是 glob 工具的输入参数。
type GlobRequest struct {
	Pattern string `json:"pattern" jsonschema:"required,description=The glob pattern to match files against"`
	Path    string `json:"path" jsonschema:"description=The directory to search in. If not specified, uses current working directory"`
}

// GlobResult is the output of the glob tool.
// GlobResult 是 glob 工具的输出结果。
type GlobResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// globExec executes the glob pattern matching and returns matching files.
// globExec 执行 glob 模式匹配并返回匹配的文件。
func globExec(ctx context.Context, req *GlobRequest) (*GlobResult, error) {
	path := req.Path
	if path == "" {
		path = defaultPath
	}

	// Resolve the base directory.
	// 解析基础目录。
	base, err := filepath.Abs(path)
	if err != nil {
		return &GlobResult{
			Content: "Error: Invalid path: " + err.Error(),
			IsError: true,
		}, nil
	}

	// Check if directory exists.
	// 检查目录是否存在。
	info, err := os.Stat(base)
	if err != nil {
		if os.IsNotExist(err) {
			return &GlobResult{
				Content: "Error: Directory not found: " + path,
				IsError: true,
			}, nil
		}
		return &GlobResult{
			Content: "Error: Cannot access path: " + err.Error(),
			IsError: true,
		}, nil
	}
	if !info.IsDir() {
		return &GlobResult{
			Content: "Error: Path is not a directory: " + path,
			IsError: true,
		}, nil
	}

	// Execute glob pattern.
	// 执行 glob 模式匹配。
	matches, err := globMatch(base, req.Pattern)
	if err != nil {
		return &GlobResult{
			Content: "Error: Invalid pattern: " + err.Error(),
			IsError: true,
		}, nil
	}

	if len(matches) == 0 {
		return &GlobResult{
			Content: "No files found matching the pattern.",
			IsError: false,
		}, nil
	}

	// Sort by modification time (newest first).
	// 按修改时间排序（最新的在前）。
	sort.Slice(matches, func(i, j int) bool {
		infoI, errI := os.Stat(matches[i])
		infoJ, errJ := os.Stat(matches[j])
		if errI != nil || errJ != nil {
			return matches[i] < matches[j]
		}
		return infoI.ModTime().After(infoJ.ModTime())
	})

	// Check if truncation is needed.
	// 检查是否需要截断。
	truncated := len(matches) > maxResults
	if truncated {
		matches = matches[:maxResults]
	}

	// Convert to relative paths to save tokens.
	// 转换为相对路径以节省 tokens。
	relMatches := make([]string, 0, len(matches))
	for _, m := range matches {
		rel, err := filepath.Rel(base, m)
		if err != nil {
			relMatches = append(relMatches, m)
		} else {
			relMatches = append(relMatches, rel)
		}
	}

	result := strings.Join(relMatches, "\n")
	if truncated {
		result += "\n(Results are truncated. Consider using a more specific path or pattern.)"
	}

	return &GlobResult{Content: result, IsError: false}, nil
}

// globMatch matches files against a glob pattern, supporting ** for recursive matching.
// globMatch 使用 glob 模式匹配文件，支持 ** 递归匹配。
func globMatch(base, pattern string) ([]string, error) {
	// Check if pattern contains ** for recursive matching.
	// 检查模式是否包含 ** 以进行递归匹配。
	if strings.Contains(pattern, "**") {
		return globRecursive(base, pattern)
	}

	// Simple pattern: use filepath.Glob.
	// 简单模式：使用 filepath.Glob。
	return filepath.Glob(filepath.Join(base, pattern))
}

// globRecursive handles patterns with ** for recursive directory matching.
// globRecursive 处理包含 ** 的模式以进行递归目录匹配。
func globRecursive(base, pattern string) ([]string, error) {
	// Convert glob ** pattern to a form we can process.
	// 将 glob ** 模式转换为可处理的格式。
	parts := strings.Split(pattern, "**")

	if len(parts) == 1 {
		// Pattern is just **/*.go
		// 模式只是 **
		return globRecursiveAll(base, parts[0])
	}

	// Pattern like "src/**/*.go"
	// 模式如 "src/**/*.go"
	before := parts[0]
	after := strings.TrimPrefix(parts[1], "/")
	depth := strings.Count(before, "/")

	// Calculate how many directory levels to walk.
	// 计算要遍历的目录层级。
	minDepth := depth
	maxDepth := 100 // Arbitrary large number for unlimited.

	return globRecursiveWalk(base, before, after, minDepth, maxDepth)
}

// globRecursiveAll walks all directories recursively and applies the suffix pattern.
// globRecursiveAll 递归遍历所有目录并应用后缀模式。
func globRecursiveAll(base, suffix string) ([]string, error) {
	var matches []string
	suffix = strings.TrimPrefix(suffix, "/")

	err := filepath.WalkDir(base, func(path string, info os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip inaccessible paths.
		}
		if info.IsDir() {
			return nil
		}
		// Check if file matches suffix pattern.
		// 检查文件是否匹配后缀模式。
		matched, err := filepath.Match(suffix, filepath.Base(path))
		if err != nil {
			return nil
		}
		if matched {
			matches = append(matches, path)
		}
		return nil
	})

	return matches, err
}

// globRecursiveWalk walks directories and matches files with the given pattern.
// globRecursiveWalk 遍历目录并使用给定模式匹配文件。
func globRecursiveWalk(base, prefix, suffix string, minDepth, maxDepth int) ([]string, error) {
	var matches []string
	prefix = strings.Trim(prefix, "/")

	searchDir := base
	if prefix != "" {
		searchDir = filepath.Join(base, prefix)
		if _, err := os.Stat(searchDir); os.IsNotExist(err) {
			return matches, nil
		}
	}

	err := filepath.WalkDir(searchDir, func(path string, info os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		currentDepth := strings.Count(path, string(filepath.Separator)) - strings.Count(base, string(filepath.Separator))

		if info.IsDir() {
			// Skip directories that are shallower than minDepth.
			// 跳过浅于 minDepth 的目录。
			if currentDepth < minDepth {
				return nil
			}
			// Skip directories that exceed maxDepth.
			// 跳过超过 maxDepth 的目录。
			if currentDepth > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}

		// Check if file matches suffix pattern.
		// 检查文件是否匹配后缀模式。
		matched, err := filepath.Match(suffix, filepath.Base(path))
		if err != nil {
			return nil
		}
		if matched {
			matches = append(matches, path)
		}
		return nil
	})

	return matches, err
}

// NewGlobTool creates an InvokableTool for glob pattern matching.
// NewGlobTool 创建一个可调用的 glob 模式匹配工具。
func NewGlobTool() (tool.InvokableTool, error) {
	return utils.InferTool("Glob",
		"Fast file pattern matching tool that works with any codebase size.\n\n"+
			"- Supports glob patterns like '**/*.go' or 'src/**/*.ts'\n"+
			"- Returns matching file paths sorted by modification time\n"+
			"- Use this tool when you need to find files by name patterns\n\n"+
			"Usage:\n"+
			"- pattern: The glob pattern to match (e.g., '**/*.go', '*.json', 'src/**/*.ts')\n"+
			"- path: Optional directory to search in (defaults to current directory)\n\n"+
			"Examples:\n"+
			"- pattern='**/*.go' - Find all Go files\n"+
			"- pattern='*.json' - Find JSON files in current directory\n"+
			"- pattern='**/*.test.js' path='src' - Find test files in src directory",
		globExec)
}
