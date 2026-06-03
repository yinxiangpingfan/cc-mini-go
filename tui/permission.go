package main

import (
	"context"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yinxiangpingfan/cc-mini-go/agent"
	"github.com/yinxiangpingfan/cc-mini-go/agent/core"
	"github.com/yinxiangpingfan/cc-mini-go/config"
)

// defaultMode 是 TUI 未在 setting.json 指定 permission.mode 时的默认权限模式：
// 只读工具放行、写/命令类弹确认，最不打扰又有护栏。
const defaultMode = core.ModeAuto

// buildEngine 依据 setting.json 的 permission 段构造权限引擎。
// 始终叠加内置 bash deny 规则（NewPermissionEngineWithDefaults），用户规则在其后生效。
func buildEngine(cfg config.PermissionConfig) *core.PermissionEngine {
	mode := parseMode(cfg.Mode)
	deny := toRules(cfg.Deny, core.PermDeny)
	allow := toRules(cfg.Allow, core.PermAllow)
	return core.NewPermissionEngineWithDefaults(mode, deny, allow)
}

// parseMode 把配置里的模式字符串映射为 PermMode；空或未知一律降级为 defaultMode。
func parseMode(s string) core.PermMode {
	switch core.PermMode(s) {
	case core.ModeDefault:
		return core.ModeDefault
	case core.ModePlan:
		return core.ModePlan
	case core.ModeAuto:
		return core.ModeAuto
	default:
		return defaultMode
	}
}

// toRules 把配置规则转成 core.PermissionRule，Behavior 由所在桶（deny/allow）决定。
func toRules(cfgs []config.PermissionRuleConfig, behavior core.PermBehavior) []core.PermissionRule {
	if len(cfgs) == 0 {
		return nil
	}
	rules := make([]core.PermissionRule, 0, len(cfgs))
	for _, c := range cfgs {
		rules = append(rules, core.PermissionRule{
			Tool:     c.Tool,
			Content:  c.Content,
			Behavior: behavior,
		})
	}
	return rules
}

// cycleMode 返回模式切换的下一档：auto → plan → default → auto。
func cycleMode(m core.PermMode) core.PermMode {
	switch m {
	case core.ModeAuto:
		return core.ModePlan
	case core.ModePlan:
		return core.ModeDefault
	default:
		return core.ModeAuto
	}
}

// modeLabel 模式的简短中文标签，用于状态/帮助行展示。
func modeLabel(m core.PermMode) string {
	switch m {
	case core.ModePlan:
		return "plan 只读"
	case core.ModeAuto:
		return "auto 自动"
	default:
		return "default 逐次确认"
	}
}

// permissionAskMsg 是从 agent 工具 goroutine 经 program.Send 注入 Update 循环的权限询问请求。
// reply 是回传通道（带缓冲，防止用户已离开/已取消时 model 端发送阻塞）。
type permissionAskMsg struct {
	req   agent.PermissionRequest
	reply chan bool
}

// approver 把 agent 的 ApprovalFunc 桥接到 Bubble Tea：
// 工具 goroutine 调用 Approve → 经 program.Send 把请求送进 Update → 阻塞等用户按键回传。
//
// 它只依赖 *tea.Program（不依赖 model），从而打破「agent 需 approver、approver 需 program、
// program 需 model、model 需 agent」的构造环：program 在 model 之后才存在，这里延迟注入。
type approver struct {
	gate   sync.Mutex // 串行化：并发工具调用时一次只弹一个确认框
	progMu sync.RWMutex
	prog   *tea.Program
}

// setProgram 在 tea.Program 创建后注入句柄（启动期一次性调用）。
func (ap *approver) setProgram(p *tea.Program) {
	ap.progMu.Lock()
	ap.prog = p
	ap.progMu.Unlock()
}

// Approve 实现 agent.ApprovalFunc：把请求送进 UI 并阻塞等待用户决定。
// ctx 取消（用户 Ctrl+C 中断本轮）时立即按拒绝返回。
func (ap *approver) Approve(ctx context.Context, req agent.PermissionRequest) bool {
	ap.gate.Lock()
	defer ap.gate.Unlock()

	ap.progMu.RLock()
	p := ap.prog
	ap.progMu.RUnlock()
	if p == nil {
		return false // 尚未就绪，保守拒绝
	}

	reply := make(chan bool, 1)
	p.Send(permissionAskMsg{req: req, reply: reply})
	select {
	case ok := <-reply:
		return ok
	case <-ctx.Done():
		return false
	}
}
