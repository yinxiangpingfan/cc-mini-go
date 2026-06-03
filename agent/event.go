package agent

import "time"

// AgentEventType 是 agent 向外广播的事件类型。
// 目前只有重试事件，后续可按需扩展（如 token / tool_start / tool_done）。
type AgentEventType string

const (
	// EventReasoning 流式思考内容的增量 token。
	EventReasoning AgentEventType = "reasoning"
	// EventContent 助手对话内容（流式为增量 token，非流式为整段文本）。
	EventContent AgentEventType = "content"
	// EventToolStart 某个工具开始执行。
	EventToolStart AgentEventType = "tool_start"
	// EventToolResult 某个工具执行完成。
	EventToolResult AgentEventType = "tool_result"
	// EventRetry LLM 调用失败、准备重试前发出，供 UI 提示「重试中」。
	EventRetry AgentEventType = "retry"
)

// AgentEvent 是 agent 运行过程中向外（如 TUI）广播的进度事件。
// 不同事件类型只填用得到的字段，其余保持零值。
type AgentEvent struct {
	Type AgentEventType

	// 文本载荷：reasoning/content 的 token、tool_result 的工具结果。
	Text string

	// 工具类事件
	ToolID   string // 工具调用唯一 ID（tool_start / tool_result，用于精确配对，避免同名并发串台）
	ToolName string // 工具名（tool_start / tool_result）
	ToolArgs string // 工具的原始参数 JSON（tool_start）

	// 重试类事件
	Attempt int           // 第几次重试（从 1 起）
	Max     int           // 重试次数上限
	Delay   time.Duration // 本次重试前的等待时长
	Err     error         // 触发本次重试的错误
}

// AgentOption 用于在构造 ChatCompletionAgent 时注入可选能力。
type AgentOption func(*ChatCompletionAgent)

// WithEventChannel 注入一个事件 channel，agent 会把重试等进度事件投递进去。
// 事件以「尽力而为」方式非阻塞发送：消费者跟不上（channel 满）时丢弃，绝不阻塞 agent。
// 建议传入带缓冲的 channel，并在自己的循环里消费。
func WithEventChannel(ch chan<- AgentEvent) AgentOption {
	return func(a *ChatCompletionAgent) { a.events = ch }
}

// emit 非阻塞地发送一个事件；未配置 channel 时为空操作。
func (a *ChatCompletionAgent) emit(ev AgentEvent) {
	if a.events == nil {
		return
	}
	select {
	case a.events <- ev:
	default: // 消费者跟不上则丢弃，保证 agent 不被 UI 拖慢或卡死
	}
}
