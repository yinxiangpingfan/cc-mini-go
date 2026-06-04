package agent

import (
	"context"

	"github.com/yinxiangpingfan/cc-mini-go/agent/core"
)

// consecutiveDenyHint 连续被拒达到此次数即提示用户（可能卡住，建议切 plan）。
const consecutiveDenyHint = 3

// 权限系统接线层（s07 第 2 步）：把纯判定核心 core.PermissionEngine 接进工具循环。
// 工具调用在真正执行前先过这道闸；deny/ask 拒绝时仍回传一条配对的 tool 结果，
// 否则 assistant 的 tool_call 缺少对应 result，下一轮请求会被 API 拒绝（协议要求一一配对）。

// PermissionRequest 描述一次待确认的工具调用，传给 ApprovalFunc 供其决定放行与否。
type PermissionRequest struct {
	ToolID   string
	ToolName string
	RawArgs  string
	Args     map[string]any
	Reason   string // 触发询问的原因（来自 PermissionDecision）
}

// ApprovalFunc 在权限判定为 ask 时被调用，返回 true 放行、false 拒绝。
// ctx 取消（如用户中断）应使其尽快返回 false。人机交互的具体实现见后续步骤。
type ApprovalFunc func(ctx context.Context, req PermissionRequest) bool

// WithPermissions 注入权限引擎。未注入（nil）时完全跳过权限检查，保持原有行为。
func WithPermissions(e *core.PermissionEngine) AgentOption {
	return func(a *ChatCompletionAgent) { a.perms = e }
}

// WithApproval 注入 ask 的人机确认回调。未注入时 ask 一律按拒绝处理（fail-closed）。
func WithApproval(fn ApprovalFunc) AgentOption {
	return func(a *ChatCompletionAgent) { a.approve = fn }
}

// gateToolCall 在工具真正执行前做权限判定。
// 返回 (deniedResult, true) 表示被拦截：调用方应把 deniedResult 作为 tool 消息回传、跳过执行；
// 返回 ("", false) 表示放行：调用方照常执行工具。
func (a *ChatCompletionAgent) gateToolCall(ctx context.Context, toolID, name, rawArgs string, args map[string]any) (string, bool) {
	if a.perms == nil {
		return "", false
	}
	d := a.perms.Check(name, args)
	switch d.Behavior {
	case core.PermDeny:
		a.recordDeny()
		return core.DeniedResult(d.Reason), true
	case core.PermAsk:
		if a.approve != nil && a.approve(ctx, PermissionRequest{
			ToolID:   toolID,
			ToolName: name,
			RawArgs:  rawArgs,
			Args:     args,
			Reason:   d.Reason,
		}) {
			a.recordAllow()
			return "", false
		}
		a.recordDeny()
		return core.DeniedResult("denied by user"), true
	default: // allow
		a.recordAllow()
		return "", false
	}
}

// recordDeny 累加连续拒绝计数；达到阈值则发出一次提示并清零（便于再累计、再提示）。
func (a *ChatCompletionAgent) recordDeny() {
	a.permMu.Lock()
	a.denyStreak++
	hit := a.denyStreak >= consecutiveDenyHint
	if hit {
		a.denyStreak = 0
	}
	a.permMu.Unlock()
	if hit {
		a.emit(AgentEvent{
			Type: EventPermissionHint,
			Text: "连续多次工具调用被拒绝，可能卡住了——可切到 plan 模式（只读）重新梳理，或澄清目标后再试。",
		})
	}
}

// recordAllow 任一放行即清零连续拒绝计数（"连续"以放行为断点）。
func (a *ChatCompletionAgent) recordAllow() {
	a.permMu.Lock()
	a.denyStreak = 0
	a.permMu.Unlock()
}
