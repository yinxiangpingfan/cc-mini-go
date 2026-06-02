package agent_tools

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

// skillFileName 是每个 skill 目录下约定的说明书文件名
const skillFileName = "SKILL.md"

// SkillManifest 是一份 skill 的轻量元信息（发现层用）。
// 只描述「有这份 skill、它大概干啥」，不含完整正文，几乎不占 token。
type SkillManifest struct {
	Name        string
	Description string
	Path        string
}

// SkillDocument 是一份 skill 的完整内容（加载层用）。
// 只有当模型真正需要时，Body 才会被注入上下文。
type SkillDocument struct {
	Manifest SkillManifest
	Body     string
}

// SkillRegistry 统一管理所有可用 skill。
// 它回答两个问题：有哪些 skill 可用、某个 skill 的完整内容是什么。
type SkillRegistry struct {
	Documents map[string]SkillDocument
}

// NewSkillRegistry 从给定目录加载所有 SKILL.md。
// dirs 按顺序加载，后面的目录在同名时覆盖前面的（项目级覆盖全局级）。
// 不存在的目录会被静默跳过。
func NewSkillRegistry(dirs ...string) *SkillRegistry {
	r := &SkillRegistry{Documents: make(map[string]SkillDocument)}
	for _, dir := range dirs {
		r.loadDir(dir)
	}
	return r
}

// loadDir 递归扫描 dir 下所有 SKILL.md 并注册。
func (r *SkillRegistry) loadDir(dir string) {
	if dir == "" {
		return
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return
	}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 跳过无法访问的项，不中断整体扫描
		}
		if d.IsDir() || d.Name() != skillFileName {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		meta, body := parseFrontmatter(string(raw))
		name := meta["name"]
		if name == "" {
			// 没写 name 就用所在目录名兜底
			name = filepath.Base(filepath.Dir(path))
		}
		description := meta["description"]
		if description == "" {
			description = "No description"
		}
		r.Documents[name] = SkillDocument{
			Manifest: SkillManifest{Name: name, Description: description, Path: path},
			Body:     strings.TrimSpace(body),
		}
		return nil
	})
}

// parseFrontmatter 解析正文前的一小段元数据。
// 约定格式：
//
//	---
//	name: code-review
//	description: ...
//	---
//	正文...
//
// 支持 YAML 块标量（description: | 后跟缩进多行），会折叠成单行。
// 没有 frontmatter 时返回空元数据和原文。
func parseFrontmatter(text string) (map[string]string, string) {
	meta := make(map[string]string)
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return meta, normalized
	}
	rest := normalized[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end == -1 {
		return meta, normalized
	}
	front := rest[:end]
	body := rest[end+len("\n---"):]
	body = strings.TrimPrefix(body, "\n") // 去掉闭合 fence 后的换行
	body = strings.TrimPrefix(body, "\n") // 兼容 "---\n\n正文"

	lines := strings.Split(front, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		// 缩进行属于上一个块标量的内容，这里跳过（已被块标量逻辑消费）
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		idx := strings.Index(line, ":")
		if idx == -1 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		if key == "" {
			continue
		}

		// 块标量：值为 | 或 > （及其变体）时，收集后续缩进行并折叠成单行
		if isBlockScalarIndicator(value) {
			block := make([]string, 0, 4)
			j := i + 1
			for j < len(lines) {
				next := lines[j]
				if next == "" {
					block = append(block, "")
					j++
					continue
				}
				if next[0] == ' ' || next[0] == '\t' {
					block = append(block, strings.TrimSpace(next))
					j++
					continue
				}
				break // 遇到下一个非缩进字段，块结束
			}
			// strings.Fields 折叠所有空白（含空行），得到干净的单行描述
			value = strings.Join(strings.Fields(strings.Join(block, " ")), " ")
			i = j - 1 // 外层 i++ 后从 j 继续
		}

		meta[key] = value
	}
	return meta, body
}

// isBlockScalarIndicator 判断值是否是 YAML 块标量起始标记。
func isBlockScalarIndicator(value string) bool {
	switch value {
	case "|", ">", "|-", ">-", "|+", ">+":
		return true
	default:
		return false
	}
}

// DescribeAvailable 返回轻量的 skill 目录（发现层）。
// 这是放进 system prompt 的内容——只有名字和描述，不含正文。
func (r *SkillRegistry) DescribeAvailable() string {
	if len(r.Documents) == 0 {
		return "(no skills available)"
	}
	names := make([]string, 0, len(r.Documents))
	for name := range r.Documents {
		names = append(names, name)
	}
	sort.Strings(names)

	lines := make([]string, 0, len(names))
	for _, name := range names {
		m := r.Documents[name].Manifest
		lines = append(lines, fmt.Sprintf("- %s: %s", m.Name, m.Description))
	}
	return strings.Join(lines, "\n")
}

// LoadFullText 返回某个 skill 的完整正文（加载层），用 <skill> 标签包裹。
// 找不到时返回带可用列表的错误说明，便于模型纠正。
func (r *SkillRegistry) LoadFullText(name string) string {
	doc, ok := r.Documents[name]
	if !ok {
		known := "(none)"
		if len(r.Documents) > 0 {
			names := make([]string, 0, len(r.Documents))
			for n := range r.Documents {
				names = append(names, n)
			}
			sort.Strings(names)
			known = strings.Join(names, ", ")
		}
		return jsonErr(fmt.Sprintf(errors.ErrUnknownSkill, name, known))
	}
	return fmt.Sprintf("<skill name=\"%s\">\n%s\n</skill>", doc.Manifest.Name, doc.Body)
}

// DefaultSkillDirs 返回默认的 skill 搜索目录：先全局，后项目（项目覆盖全局）。
// 全局：~/.cc_mini_go/skills    项目：<cwd>/.cc_mini_go/skills
func DefaultSkillDirs() []string {
	dirs := make([]string, 0, 2)
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".cc_mini_go", "skills"))
	}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, filepath.Join(cwd, ".cc_mini_go", "skills"))
	}
	return dirs
}

// Skills 是包级默认注册表，从全局 + 项目目录加载。
var Skills = NewSkillRegistry(DefaultSkillDirs()...)

// NewLoadSkillTool 返回 load_skill 工具：按需把某份 skill 正文加载进上下文。
func NewLoadSkillTool(registry *SkillRegistry) *Tools {
	return &Tools{
		Name: "load_skill",
		Func: func(args map[string]any) string {
			name, ok := args["name"].(string)
			if !ok || name == "" {
				return jsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "name"))
			}
			return registry.LoadFullText(name)
		},
	}
}

func (t *Tools) LoadSkillInfoForLLM() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "load_skill",
			Description: prompt.LoadSkillPrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"name": client.ParameterProperty{
						Type:        "string",
						Description: "The name of the skill to load, as listed in the available skills catalog.",
					},
				},
				Required: []string{"name"},
			},
		},
	}
}
