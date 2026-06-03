package agent

import (
	"log/slog"

	"github.com/yinxiangpingfan/cc-mini-go/agent/core"
)

// 内置 hook 集中地（s08 第 3 步）。
//
// ★ 要增删内置 hook，只改这一个文件：在 registerBuiltinHooks 里加一行注册，
//   再在下面写一个 a.hookXxx 方法即可——不必去碰主循环、event、tui 任何地方。★
//
// 内置 handler 一律写成 ChatCompletionAgent 的方法（闭包持有 a），这样它们能：
//   - 通过 a.emit(...) 把提示投递给 TUI（而不是 fmt.Println 污染界面）；
//   - 通过 slog 写入 ~/.cc_mini_go/tui.log。

// WithBuiltinHooks 开启内置 hook。未开启时不注册任何内置 handler，保持原有行为（零侵入）。
// 可与 WithHooks 叠加：内置 handler 会追加到自定义 runner 上。
func WithBuiltinHooks() AgentOption {
	return func(a *ChatCompletionAgent) { a.builtinHooks = true }
}

// registerBuiltinHooks 注册全部内置 hook。这就是「填 handler 的唯一入口」。
func (a *ChatCompletionAgent) registerBuiltinHooks() {
	if a.hooks == nil {
		a.hooks = core.NewHookRunner()
	}
	a.hooks.Register(core.HookSessionStart, a.hookWelcome)
	//a.hooks.Register(core.HookPostToolUse, a.hookAuditLog)
	// 在此继续追加：a.hooks.Register(core.HookPreToolUse, a.hookXxx)
}

// fireSessionStart 触发会话级 hook，整个会话仅一次（由 sessionOnce 守护）。
// SessionStart 只为副作用而跑（欢迎语 / 预热），其返回值不参与主循环判定。
func (a *ChatCompletionAgent) fireSessionStart() {
	a.sessionOnce.Do(func() {
		payload := map[string]any{}
		if a.cf != nil {
			payload["model"] = a.cf.Model
		}
		a.runHook(core.HookSessionStart, payload)
	})
}

// ——以下是内置 handler 本体——

// hookWelcome 会话开始时向 UI 发一条提示（观察型，不干预流程）。
func (a *ChatCompletionAgent) hookWelcome(_ map[string]any) core.HookResult {
	a.emit(AgentEvent{Type: EventHookNotice, Text: "🪝 会话已启动，hook 系统已就绪"})
	return core.HookResult{ExitCode: core.HookContinue}
}

// hookAuditLog 每个工具执行后记一条审计日志（写入 tui.log，不打扰 UI）。
func (a *ChatCompletionAgent) hookAuditLog(p map[string]any) core.HookResult {
	name, _ := p["tool_name"].(string)
	slog.Info("hook audit: tool finished", "tool", name)
	return core.HookResult{ExitCode: core.HookContinue}
}
