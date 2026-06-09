package fileops

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
	"github.com/yinxiangpingfan/cc-mini-go/tools"
)

// grepDefaultHeadLimit 默认最多返回的结果条数
const grepDefaultHeadLimit = 250

// grepSearch 用 Go 正则在 root（文件或目录）下搜索 pattern，纯标准库实现。
// 返回结果条目、是否因 headLimit 截断、错误。会跳过 .git 目录与二进制文件。
func grepSearch(ctx context.Context, pattern, root, globPat, outputMode string, ignoreCase, lineNumbers, multiline bool, headLimit int) ([]string, bool, error) {
	// 组装正则前缀标志：忽略大小写 / 多行（dot-all，让 . 匹配换行）
	var prefix string
	if ignoreCase {
		prefix += "(?i)"
	}
	if multiline {
		prefix += "(?s)"
	}
	re, err := regexp.Compile(prefix + pattern)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", errors.ErrInvalidRegex, err)
	}

	info, err := os.Stat(root)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", errors.ErrSearchPath, err)
	}

	// 收集待搜索文件
	var files []string
	if info.IsDir() {
		walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return filepath.SkipAll // 被取消则尽快停止遍历
			}
			if err != nil {
				return nil // 跳过无法访问的项，不中断整体扫描
			}
			if d.IsDir() {
				if d.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if globPat != "" {
				if ok, _ := filepath.Match(globPat, d.Name()); !ok {
					return nil
				}
			}
			files = append(files, p)
			return nil
		})
		if walkErr != nil {
			return nil, false, fmt.Errorf("%w: %w", errors.ErrSearchPath, walkErr)
		}
	} else {
		// 单个文件：若设置了 glob 仍按文件名过滤
		if globPat != "" {
			if ok, _ := filepath.Match(globPat, info.Name()); !ok {
				return nil, false, nil
			}
		}
		files = append(files, root)
	}
	sort.Strings(files)

	var results []string
	truncated := false
	// appendResult 追加一条结果，达到上限时返回 false 通知停止
	appendResult := func(s string) bool {
		results = append(results, s)
		if headLimit > 0 && len(results) >= headLimit {
			truncated = true
			return false
		}
		return true
	}

outer:
	for _, f := range files {
		// 被取消则停止处理
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		// 跳过二进制文件
		if bin, _, berr := tools.IsBinaryFile(f); berr == nil && bin {
			continue
		}
		data, rerr := os.ReadFile(f)
		if rerr != nil {
			continue // 读不了就跳过，不影响其他文件
		}
		content := string(data)

		switch outputMode {
		case "count":
			if n := len(re.FindAllStringIndex(content, -1)); n > 0 {
				if !appendResult(fmt.Sprintf("%s:%d", f, n)) {
					break outer
				}
			}
		case "content":
			if multiline {
				// 多行模式：在整段内容上找匹配，行号取匹配起始位置
				for _, loc := range re.FindAllStringIndex(content, -1) {
					matchText := content[loc[0]:loc[1]]
					line := 1 + strings.Count(content[:loc[0]], "\n")
					if !appendResult(formatContentLine(f, line, matchText, lineNumbers)) {
						break outer
					}
				}
			} else {
				for i, line := range strings.Split(content, "\n") {
					if re.MatchString(line) {
						if !appendResult(formatContentLine(f, i+1, line, lineNumbers)) {
							break outer
						}
					}
				}
			}
		default: // files_with_matches
			if re.MatchString(content) {
				if !appendResult(f) {
					break outer
				}
			}
		}
	}
	return results, truncated, nil
}

// formatContentLine 组装 content 模式下的一行输出（可选行号）。
func formatContentLine(file string, line int, text string, withLineNumber bool) string {
	if withLineNumber {
		return fmt.Sprintf("%s:%d:%s", file, line, text)
	}
	return fmt.Sprintf("%s:%s", file, text)
}

func NewGrepTool() *Tools {
	return &Tools{
		Name: "grep",
		Func: func(ctx context.Context, args map[string]any) string {
			pattern, ok := args["pattern"].(string)
			if !ok || pattern == "" {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "pattern"))
			}
			path, ok := args["path"].(string)
			if !ok || path == "" {
				path = "."
			}
			globPat, _ := args["glob"].(string)

			outputMode, _ := args["output_mode"].(string)
			if outputMode == "" {
				outputMode = "files_with_matches"
			}
			switch outputMode {
			case "files_with_matches", "content", "count":
			default:
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "output_mode"))
			}

			ignoreCase, _ := args["-i"].(bool)
			lineNumbers := true
			if v, ok := args["-n"].(bool); ok {
				lineNumbers = v
			}
			multiline, _ := args["multiline"].(bool)

			headLimit := grepDefaultHeadLimit
			if v, ok := args["head_limit"].(float64); ok && int(v) > 0 {
				headLimit = int(v)
			}

			results, truncated, err := grepSearch(ctx, pattern, path, globPat, outputMode, ignoreCase, lineNumbers, multiline, headLimit)
			if err != nil {
				return shared.JsonErr(err.Error())
			}
			b, _ := json.Marshal(map[string]any{
				"mode":      outputMode,
				"matches":   results,
				"count":     len(results),
				"truncated": truncated,
			})
			return string(b)
		},
	}
}

func (t *Tools) GrepInfoForLLm() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "grep",
			Description: prompt.GrepPrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"pattern": client.ParameterProperty{
						Type:        "string",
						Description: "The regular expression to search for (Go RE2 syntax)",
					},
					"path": client.ParameterProperty{
						Type:        "string",
						Description: "File or directory to search in (default: current directory)",
					},
					"glob": client.ParameterProperty{
						Type:        "string",
						Description: "Filter files by name pattern, e.g. '*.go'",
					},
					"output_mode": client.ParameterProperty{
						Type:        "string",
						Enum:        []string{"files_with_matches", "content", "count"},
						Description: "files_with_matches (default), content, or count",
					},
					"-i": client.ParameterProperty{
						Type:        "boolean",
						Description: "Case-insensitive matching",
					},
					"-n": client.ParameterProperty{
						Type:        "boolean",
						Description: "Show line numbers in content mode (default true)",
					},
					"multiline": client.ParameterProperty{
						Type:        "boolean",
						Description: "Allow the pattern to match across line boundaries",
					},
					"head_limit": client.ParameterProperty{
						Type:        "integer",
						Description: "Cap the number of returned entries (default 250)",
					},
				},
				Required: []string{"pattern"},
			},
		},
	}
}
