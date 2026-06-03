package core

// Hook 系统：让主循环在固定时机「对外发出调用」，不改主循环主体即可扩展行为。
// 与 permission.go 同属 core 策略层，只依赖标准库；不含具体 handler（示例 handler 由上层注册）。

// 没有用锁：约定 handler 一律在启动时注册完，运行期只 Run（只读）。Go 的 map 并发只读安全，
// 因此 Run 可被工具的并发 goroutine 同时调用；但不要在会话运行中再 Register。

import "encoding/json"

// Hook 事件名：主循环暴露的三个时机（先只讲这三个）。
const (
	HookSessionStart = "SessionStart" // 会话开始（整个会话仅一次）
	HookPreToolUse   = "PreToolUse"   // 工具执行前
	HookPostToolUse  = "PostToolUse"  // 工具执行后
)

// Hook 退出码：统一约定的三种作用——观察 / 拦截 / 补充。
const (
	HookContinue = 0 // 正常继续
	HookBlock    = 1 // 阻止当前动作
	HookInject   = 2 // 注入一条补充消息，再继续
)

// HookResult 是一次 hook 的处置：要不要拦、要不要向模型补一条说明。
type HookResult struct {
	ExitCode int    // HookContinue / HookBlock / HookInject
	Message  string // 拦截原因，或 HookInject 时要注入的补充说明
}

// HookBlockedResult 构造一条「被 hook 拦截」的工具结果（JSON 字符串），与其它工具错误同构，
// 可直接作为 role:"tool" 消息回传给模型——模型看到 error 会自行改道。与 DeniedResult 对称。
func HookBlockedResult(reason string) string {
	if reason == "" {
		reason = "blocked by hook"
	}
	b, _ := json.Marshal(map[string]string{"error": "blocked by hook: " + reason})
	return string(b)
}

// HookHandler 处理某次事件的上下文（tool_name / input / output 等）并返回处置。
type HookHandler func(payload map[string]any) HookResult

// HookRunner 持有「事件名 → 一组 handler」的注册表，对主循环屏蔽各 handler 的细节：
// 主循环只把事件名 + payload 交给它，由它统一调度。
type HookRunner struct {
	hooks map[string][]HookHandler
}

// NewHookRunner 构造一个空的运行器。
func NewHookRunner() *HookRunner {
	return &HookRunner{hooks: map[string][]HookHandler{}}
}

// Register 给某个事件追加一个 handler；同一事件按注册顺序触发。nil 忽略。仅在启动时调用。
func (r *HookRunner) Register(event string, h HookHandler) {
	if h != nil {
		r.hooks[event] = append(r.hooks[event], h)
	}
}

// Run 按注册顺序触发某事件的全部 handler。
// 谁先返回「拦截(1)」或「注入(2)」谁优先，立即返回并短路其余；全返回「继续(0)」则返回零值。
// 未注册任何 handler 等价于继续。
func (r *HookRunner) Run(event string, payload map[string]any) HookResult {
	for _, h := range r.hooks[event] {
		if res := h(payload); res.ExitCode != HookContinue {
			return res
		}
	}
	return HookResult{ExitCode: HookContinue}
}
