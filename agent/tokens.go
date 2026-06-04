package agent

import "github.com/yinxiangpingfan/cc-mini-go/agent_tools"

// coldStartOverheadTokens 是冷启动估算时给 system prompt + 工具 schema 的粗略补偿。
// 这两部分不在 allMsg 里（分别走 system 字段与 tools 字段），纯估算数不到，故补一笔固定值。
const coldStartOverheadTokens = 1000

// estimateContextTokens 估算当前上下文的 token 数，用于按 token（而非字节）判定是否压缩。
//
// 优先用 API 的权威值：
//   - 已有基线（本轮调过 API 且未因压缩失效，sentCount 标出上次发送边界）
//     → lastPromptTokens + 估算(自上次发送后新增的尾巴)。
//     lastPromptTokens 已含 system / 工具 schema / 全部历史，精确；只需估那一小截新消息。
//   - 否则（冷启动 / 压缩后首轮，sentCount==0）
//     → 全量估算 + 固定开销补 system/schema。
func (a *ChatCompletionAgent) estimateContextTokens(allMsg []any, sentCount int) int {
	if a.lastPromptTokens > 0 && sentCount > 0 && sentCount <= len(allMsg) {
		return a.lastPromptTokens + agent_tools.EstimateTokens(allMsg[sentCount:])
	}
	return agent_tools.EstimateTokens(allMsg) + coldStartOverheadTokens
}
