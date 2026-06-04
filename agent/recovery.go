package agent

import (
	stderrors "errors"
	"log/slog"

	perrors "github.com/yinxiangpingfan/cc-mini-go/errors"
)

// 错误恢复（s11）：把「报错就崩」升级为「先分类错误，再选恢复路径」。
//
// 分层说明：transport 层的退避重试（超时/限流/5xx）已在 retry.go 的内层循环处理；
// 本文件只负责外层两条路径——输出截断后续写、上下文超长后压缩重试——外加最终失败。
// 到达这里的 err 已是「退避重试耗尽或不可重试」的结果。

// 恢复预算：每条路径各算各的次数，防止主循环无限「续写/压缩」。
const (
	maxContinueAttempts = 3 // 输出截断后最多连续续写次数
	maxCompactRecovery  = 2 // 上下文超长后最多压缩重试次数
)

// recoveryState 记录各恢复路径已用预算（单次 Agent/StreamAgent 调用内有效）。
type recoveryState struct {
	continueAttempts int // 连续续写次数（一旦有正常进展即清零）
	compactAttempts  int // 压缩恢复次数（本次调用累计）
}

// recoveryKind 是一次恢复决策的结果。
type recoveryKind int

const (
	recoveryNone     recoveryKind = iota // 无需恢复：正常继续
	recoveryContinue                     // 输出被截断：注入续写提示再来一轮
	recoveryCompact                      // 上下文超长：压缩历史后重试
	recoveryFail                         // 不可恢复：把错误暴露给用户
)

// chooseRecovery 把「错误/截断长什么样」映射到「该走哪条恢复路径」。纯函数，便于测试。
//   - 调用成功但 finish_reason==length → 续写
//   - 调用失败且是上下文超长 → 压缩
//   - 其他调用失败 → 失败
//   - 其余 → 无需恢复
func chooseRecovery(finishReason string, err error) (recoveryKind, string) {
	switch {
	case err == nil:
		if finishReason == "length" {
			return recoveryContinue, "output truncated"
		}
		return recoveryNone, ""
	case stderrors.Is(err, perrors.ErrContextTooLong):
		return recoveryCompact, "context too large"
	default:
		return recoveryFail, "non-recoverable error"
	}
}

// noteRecovery 让恢复动作在日志与 UI 都看得见（教学强调「恢复过程要可见」）。
func (a *ChatCompletionAgent) noteRecovery(msg string) {
	slog.Info("[recovery] " + msg)
	a.emit(AgentEvent{Type: EventRecovery, Text: msg})
}
