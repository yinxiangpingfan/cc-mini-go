package agent

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/ctxmgmt"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

// buildSystemPrompt 把 system prompt 当作一条分段组装的流水线，而非一坨硬编码文本（s10）。
//
// 静态块（相对稳定，可被 prompt cache 命中）：
//
//	core(调用方传入) + skills 目录 + memory 正文 + 分层 CLAUDE.md
//
// 动态块（每轮都可能变，放在边界标记之下）：date / cwd / model / 权限模式。
//
// 注意：工具说明不进 prompt 文本——本项目按 OpenAI 协议用单独的 tools 字段携带
// （ToolInit 返回的 []client.Tool），再在 prompt 里重列纯属 token 浪费。
//
// 每段各自只负责一种来源、空则跳过，便于单独测试与维护。
func (a *ChatCompletionAgent) buildSystemPrompt(core string) string {
	static := joinNonEmpty("\n\n",
		core,
		a.sectionSkills(),
		a.sectionMemory(),
		a.sectionClaudeMD(),
	)
	dynamic := a.sectionDynamic()
	if dynamic == "" {
		return static
	}
	return static + "\n\n" + prompt.DynamicContextHeader + "\n" + dynamic
}

// sectionSkills 轻量 skill 目录（发现层）：只放名称+描述，正文由模型按需 load_skill。
func (a *ChatCompletionAgent) sectionSkills() string {
	catalog := ctxmgmt.Skills.DescribeAvailable()
	if catalog == "(no skills available)" {
		return ""
	}
	return prompt.SkillCatalogHeader + "\n" + catalog
}

// sectionMemory 跨会话记忆（读取层）：记忆很小且是长期方向，正文全部注入。
func (a *ChatCompletionAgent) sectionMemory() string {
	section := ctxmgmt.Memory.Describe()
	if section == "" {
		return ""
	}
	return prompt.MemoryHeader + "\n" + section
}

// claudeMDSource 是一个 CLAUDE.md 来源：人类可读的层级标签 + 文件路径。
type claudeMDSource struct {
	label string
	path  string
}

// claudeMDSources 返回分层的 CLAUDE.md 来源：全局 ~/.cc_mini_go/CLAUDE.md → 项目 <cwd>/CLAUDE.md。
// 顺序即叠加顺序（靠后的更贴近当前项目），不互相覆盖。
func claudeMDSources() []claudeMDSource {
	srcs := make([]claudeMDSource, 0, 2)
	if home, err := os.UserHomeDir(); err == nil {
		srcs = append(srcs, claudeMDSource{"user", filepath.Join(home, ".cc_mini_go", "CLAUDE.md")})
	}
	if cwd, err := os.Getwd(); err == nil {
		srcs = append(srcs, claudeMDSource{"project", filepath.Join(cwd, "CLAUDE.md")})
	}
	return srcs
}

// sectionClaudeMD 读取分层 CLAUDE.md 并按来源叠加。任何一层缺失/为空都静默跳过；全空返回空串。
func (a *ChatCompletionAgent) sectionClaudeMD() string {
	blocks := make([]string, 0, 2)
	for _, src := range claudeMDSources() {
		data, err := os.ReadFile(src.path)
		if err != nil {
			continue // 不存在/不可读：该层没有指令，跳过
		}
		body := strings.TrimSpace(string(data))
		if body == "" {
			continue
		}
		blocks = append(blocks, "## "+src.label+"\n"+body)
	}
	if len(blocks) == 0 {
		return ""
	}
	return prompt.ClaudeMDHeader + "\n" + strings.Join(blocks, "\n\n")
}

// sectionDynamic 动态环境信息：每轮都可能变，故与稳定说明分开、置于边界标记之下。
func (a *ChatCompletionAgent) sectionDynamic() string {
	lines := make([]string, 0, 4)
	lines = append(lines, "- Today's date: "+time.Now().Format("2006-01-02"))
	if cwd, err := os.Getwd(); err == nil {
		lines = append(lines, "- Working directory: "+cwd)
	}
	if a.cf != nil && a.cf.Model != "" {
		lines = append(lines, "- Model: "+a.cf.Model)
	}
	if a.perms != nil {
		lines = append(lines, "- Permission mode: "+string(a.perms.Mode()))
	}
	return strings.Join(lines, "\n")
}

// joinNonEmpty 用 sep 连接非空（去掉纯空白）的片段。空段被丢弃，避免平白多出空行/空标题。
func joinNonEmpty(sep string, parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}
