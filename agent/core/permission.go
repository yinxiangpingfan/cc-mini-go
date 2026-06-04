// Package core 实现 agent 的策略核心——与具体工具解耦的纯判定逻辑。
// 目前包含权限系统（s07）：模型产生的工具调用意图，在真正执行前必须先过这条管道。
//
// 本包只做「纯判定」：三件套数据结构 + Check 四步管道，不涉及并发执行、不涉及向用户
// 询问（ask 的人机交互由 agent 层接管），也不依赖任何具体工具，便于单测覆盖。
package core

import (
	"encoding/json"
	"strings"
	"sync"
)

// PermBehavior 是对一次工具调用的处置：放行 / 拒绝 / 询问用户。
type PermBehavior string

const (
	PermAllow PermBehavior = "allow"
	PermDeny  PermBehavior = "deny"
	PermAsk   PermBehavior = "ask"
)

// PermMode 是当前会话的总体权限风格。
type PermMode string

const (
	// ModeDefault 未命中规则时询问用户——日常交互。
	ModeDefault PermMode = "default"
	// ModePlan 只读：写类工具一律拒绝——计划、审查、分析场景。
	ModePlan PermMode = "plan"
	// ModeAuto 安全只读自动放行，危险操作再询问——高流畅度探索。
	ModeAuto PermMode = "auto"
)

// readOnlyTools 是不改变外部状态的只读工具集合（mode 判定用）。
var readOnlyTools = map[string]bool{
	"read_file":  true,
	"grep":       true,
	"glob":       true,
	"time_now":   true,
	"load_skill": true,
}

// writeTools 是会改变文件系统 / 执行命令的工具集合（mode 判定用）。
var writeTools = map[string]bool{
	"write_file": true,
	"edit_file":  true,
	"bash":       true,
}

// PermissionRule 是一条权限条款：命中某工具（及可选的参数内容）时如何处置。
type PermissionRule struct {
	// Tool 针对的工具名；为空表示匹配任意工具。
	Tool string `json:"tool"`
	// Content 对工具主参数（bash 的 command、文件类的 file_path 等）做通配匹配；
	// 为空表示只按工具名匹配。* 匹配任意字符（含 / 与空格）。
	Content string `json:"content"`
	// Behavior 命中后的处置。
	Behavior PermBehavior `json:"behavior"`
}

// matches 判断本规则是否命中给定的工具调用。
func (r PermissionRule) matches(tool string, args map[string]any) bool {
	if r.Tool != "" && r.Tool != tool {
		return false
	}
	if r.Content == "" {
		return true
	}
	return globMatch(r.Content, permTarget(args))
}

// PermissionDecision 是一次判定的结果：处置 + 理由（理由便于回传给模型与排查）。
type PermissionDecision struct {
	Behavior PermBehavior
	Reason   string
}

// DeniedResult 构造一条「权限拒绝」的工具结果（JSON 字符串），与其它工具错误同构，
// 可直接作为 role:"tool" 消息回传给模型——模型看到 error 会自行改道。
func DeniedResult(reason string) string {
	b, _ := json.Marshal(map[string]string{"error": "permission denied: " + reason})
	return string(b)
}

// PermissionEngine 持有当前模式与规则集，对工具调用做线程安全的判定。
type PermissionEngine struct {
	mu         sync.RWMutex
	mode       PermMode
	denyRules  []PermissionRule
	allowRules []PermissionRule
}

// NewPermissionEngine 构造判定引擎。空模式安全降级为 ModeDefault。
func NewPermissionEngine(mode PermMode, deny, allow []PermissionRule) *PermissionEngine {
	if mode == "" {
		mode = ModeDefault
	}
	return &PermissionEngine{
		mode:       mode,
		denyRules:  deny,
		allowRules: allow,
	}
}

// DefaultBashDenyRules 返回一组内置的 bash 危险命令拦截规则。
// 教学级护栏：不做完整 shell 语法分析，只用通配（* 匹配任意字符，含 / 与空格）
// 把最明显的破坏性动作挡在执行之前。Content 多为 *X* 形式，对 command 做子串匹配，
// 因此 `cd /tmp && sudo rm -rf .` 这种拼接命令也能命中。
func DefaultBashDenyRules() []PermissionRule {
	patterns := []string{
		"*sudo *",     // 提权
		"*rm -rf*",    // 递归强删
		"*rm -fr*",    // 递归强删（flag 顺序变体）
		"*:|:&*",      // fork 炸弹（:(){ :|:& };:）
		"*mkfs*",      // 格式化文件系统
		"*/dev/sd*",   // 直接读写裸盘设备
		"*> /dev/sd*", // 重定向覆盖裸盘
	}
	rules := make([]PermissionRule, 0, len(patterns))
	for _, p := range patterns {
		rules = append(rules, PermissionRule{Tool: "bash", Content: p, Behavior: PermDeny})
	}
	return rules
}

// NewPermissionEngineWithDefaults 在内置 bash deny 规则之上叠加调用方的 deny/allow 规则。
// 内置规则排在最前（deny 按序匹配，任一命中即拒绝），保证危险命令无法被后续规则放行。
func NewPermissionEngineWithDefaults(mode PermMode, deny, allow []PermissionRule) *PermissionEngine {
	merged := append(DefaultBashDenyRules(), deny...)
	return NewPermissionEngine(mode, merged, allow)
}

// SetMode 运行期切换权限模式（线程安全）。
func (e *PermissionEngine) SetMode(mode PermMode) {
	if mode == "" {
		mode = ModeDefault
	}
	e.mu.Lock()
	e.mode = mode
	e.mu.Unlock()
}

// Mode 返回当前权限模式。
func (e *PermissionEngine) Mode() PermMode {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.mode
}

// Check 对一次工具调用做四步判定：deny → mode → allow → ask。
// 顺序固定：危险动作（deny）不交给模式决定，必须最先挡掉；安全常见操作（allow）
// 在模式之后放行；都未命中的灰区才交给用户确认（ask）。
func (e *PermissionEngine) Check(tool string, args map[string]any) PermissionDecision {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// 1. deny 规则最先——明显危险的动作优先拒绝，不让模式有机会放行。
	for _, r := range e.denyRules {
		if r.Behavior == PermDeny && r.matches(tool, args) {
			return PermissionDecision{PermDeny, "matched deny rule"}
		}
	}

	// 2. 模式——决定当前会话的大方向。
	switch e.mode {
	case ModePlan:
		if writeTools[tool] {
			return PermissionDecision{PermDeny, "plan mode blocks write tools"}
		}
	case ModeAuto:
		if readOnlyTools[tool] {
			return PermissionDecision{PermAllow, "auto mode allows read-only tools"}
		}
	}

	// 3. allow 规则——安全、重复、常见的操作直接放行。
	for _, r := range e.allowRules {
		if r.Behavior == PermAllow && r.matches(tool, args) {
			return PermissionDecision{PermAllow, "matched allow rule"}
		}
	}

	// 4. 兜底——前面都没命中的灰区交给用户确认。
	return PermissionDecision{PermAsk, "needs user confirmation"}
}

// permTargetKeys 是规则 Content 匹配时依次尝试的主参数键。
// 不同工具的「内容」落在不同字段，这里做工具无关的提取。
var permTargetKeys = []string{"command", "file_path", "path", "pattern"}

// permTarget 从工具参数里取出用于内容匹配的主字符串；都没有则返回空串。
func permTarget(args map[string]any) string {
	for _, k := range permTargetKeys {
		if v, ok := args[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// globMatch 通配匹配：与标准库 filepath.Match 不同，这里的 * 匹配任意字符
// （包含 / 与空格），更适合匹配 shell 命令与带路径的参数。
// 规则：按 * 切分，首段须为前缀、末段须为后缀，中间段须按序作为子串出现。
func globMatch(pattern, s string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == s
	}
	parts := strings.Split(pattern, "*")
	// 首段：前缀
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	rest := s[len(parts[0]):]
	// 末段：后缀
	last := parts[len(parts)-1]
	if !strings.HasSuffix(rest, last) {
		return false
	}
	// 中间段：按出现顺序逐一作为子串消费
	for _, mid := range parts[1 : len(parts)-1] {
		if mid == "" {
			continue
		}
		idx := strings.Index(rest, mid)
		if idx < 0 {
			return false
		}
		rest = rest[idx+len(mid):]
	}
	return true
}
