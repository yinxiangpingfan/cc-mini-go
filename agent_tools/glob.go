package agent_tools

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

// globMaxResults glob 返回的最大文件数
const globMaxResults = 100

// globToRegexp 把 glob 模式编译成锚定的正则。
// 支持：** 跨任意层级目录、* 单层任意字符（不含 /）、? 单个非 / 字符。
func globToRegexp(pattern string) (*regexp.Regexp, error) {
	var sb strings.Builder
	sb.WriteString("^")
	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch c {
		case '*':
			if i+1 < len(runes) && runes[i+1] == '*' {
				i++ // 吃掉第二个 *
				if i+1 < len(runes) && runes[i+1] == '/' {
					i++ // 吃掉 /，使 **/ 也能匹配零层目录
					sb.WriteString("(?:.*/)?")
				} else {
					sb.WriteString(".*")
				}
			} else {
				sb.WriteString("[^/]*")
			}
		case '?':
			sb.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			sb.WriteByte('\\')
			sb.WriteRune(c)
		default:
			sb.WriteRune(c)
		}
	}
	sb.WriteString("$")
	return regexp.Compile(sb.String())
}

// globEntry 暂存匹配到的文件，按修改时间排序用
type globEntry struct {
	rel string
	mod time.Time
}

// globSearch 在 root 目录下递归查找匹配 pattern 的文件，按修改时间倒序返回相对路径。
// 不含 / 的模式（如 *.go）匹配文件名本身（任意层级）；含 / 的模式匹配相对路径。
func globSearch(pattern, root string) ([]string, bool, error) {
	matchBase := !strings.Contains(pattern, "/")
	re, err := globToRegexp(pattern)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", errors.ErrInvalidGlobPattern, err)
	}

	info, err := os.Stat(root)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", errors.ErrSearchPath, err)
	}
	if !info.IsDir() {
		return nil, false, fmt.Errorf("%w: %s is not a directory", errors.ErrSearchPath, root)
	}

	var entries []globEntry
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 跳过无法访问的项
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		target := rel
		if matchBase {
			target = d.Name()
		}
		if re.MatchString(target) {
			var mod time.Time
			if fi, ierr := d.Info(); ierr == nil {
				mod = fi.ModTime()
			}
			entries = append(entries, globEntry{rel: rel, mod: mod})
		}
		return nil
	})
	if walkErr != nil {
		return nil, false, fmt.Errorf("%w: %w", errors.ErrSearchPath, walkErr)
	}

	// 按修改时间倒序（新→旧），时间相同按路径字典序稳定排序
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].mod.Equal(entries[j].mod) {
			return entries[i].rel < entries[j].rel
		}
		return entries[i].mod.After(entries[j].mod)
	})

	truncated := false
	if len(entries) > globMaxResults {
		entries = entries[:globMaxResults]
		truncated = true
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.rel)
	}
	return out, truncated, nil
}

func NewGlobTool() *Tools {
	return &Tools{
		Name: "glob",
		Func: func(args map[string]any) string {
			pattern, ok := args["pattern"].(string)
			if !ok || pattern == "" {
				return jsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "pattern"))
			}
			path, ok := args["path"].(string)
			if !ok || path == "" {
				path = "."
			}
			matches, truncated, err := globSearch(pattern, path)
			if err != nil {
				return jsonErr(err.Error())
			}
			b, _ := json.Marshal(map[string]any{
				"matches":   matches,
				"count":     len(matches),
				"truncated": truncated,
			})
			return string(b)
		},
	}
}

func (t *Tools) GlobInfoForLLm() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "glob",
			Description: prompt.GlobPrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"pattern": client.ParameterProperty{
						Type:        "string",
						Description: "The glob pattern to match, e.g. '**/*.go' or 'src/**/*.ts'",
					},
					"path": client.ParameterProperty{
						Type:        "string",
						Description: "Directory to search in (default: current directory)",
					},
				},
				Required: []string{"pattern"},
			},
		},
	}
}
