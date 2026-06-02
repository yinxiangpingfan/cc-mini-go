# 构建 TUI 集成指南

本文档说明如何基于 `cc-mini-go` 的 agent 事件系统构建一个 TUI（终端 UI）。
它是一份**集成契约 + 实现规范**：先把 agent 对外暴露的 API、事件模型、并发模型讲清楚，
再给出推荐的实现方式与已知限制。把本文档交给 Claude 即可据此生成具体 TUI 代码。

---

## 0. TUI 放在哪

TUI 直接用第三方库即可（Bubble Tea、lipgloss、bubbles 等），不必纠结依赖。
把它放在 `cmd/tui/`，作为核心包的**消费者**引入——核心的 `agent`/`client`/`agent_tools`
等包本身仍然只 import 标准库，只有 `cmd/tui` 这个可执行入口引入 UI 库。

下文示例用 Bubble Tea。

---

## 1. Agent 对外 API（精确签名）

### 构造

```go
// config / client / call 的标准装配（来自 test/agent_test.go）
cf, err := config.GetConfig()                 // 读 ~/.cc_mini_go/setting.json
cl, err := client.Init(cf.ApiUrl, cf.ApiKey)  // 初始化 HTTP 客户端
logger := log.InitLogger()
cm := client.NewChatCompletionMessage()
call := client.NewCall(cl, cm, logger)

// 关键：用 WithEventChannel 注入事件通道
events := make(chan agent.AgentEvent, 256)    // 大缓冲！见 §3
a := agent.NewChatCompletionAgent(&cf, call, agent.WithEventChannel(events))
```

构造函数签名（可变参数，向后兼容）：

```go
func NewChatCompletionAgent(cf *config.Config, call *client.Call, opts ...AgentOption) *ChatCompletionAgent
func WithEventChannel(ch chan<- AgentEvent) AgentOption
```

不传 `WithEventChannel` 时，agent 退回**老的 stdout 打印行为**（CLI 模式）。TUI 必须传 channel。

### 运行（两个入口，二选一）

```go
// 非流式：一次性返回，无 token 流（仍有工具/重试事件）
func (a *ChatCompletionAgent) Agent(messages []client.Message, system string) ([]any, error)

// 流式：逐 token 通过事件推送（推荐 TUI 用这个）
func (a *ChatCompletionAgent) StreamAgent(messages []client.Message, system string) ([]any, error)
```

两者都是**阻塞调用**，跑完整个「LLM↔工具」循环才返回。所以必须放在 **goroutine** 里跑（见 §3）。

- `messages`：初始消息，用 `cm.NewUserMessage("...")` 构造，例如
  `[]client.Message{*cm.NewUserMessage(userInput)}`。
- `system`：系统提示，直接用 `prompt.SystemPrompt`。
- 返回 `[]any`：本轮完整消息历史（异构：`client.Message` / `client.ToolsMessage` / `client.ResponseMessage`）。
  TUI 主要靠**事件**渲染；返回值用于「本轮结束」信号 + 拿到权威最终文本。

---

## 2. 事件模型（`AgentEvent`）

```go
type AgentEventType string

const (
    EventReasoning  AgentEventType = "reasoning"   // 流式思考增量 token
    EventContent    AgentEventType = "content"     // 助手对话内容
    EventToolStart  AgentEventType = "tool_start"  // 某工具开始执行
    EventToolResult AgentEventType = "tool_result" // 某工具执行完成
    EventRetry      AgentEventType = "retry"       // LLM 调用失败，准备重试
)

type AgentEvent struct {
    Type AgentEventType

    Text string // reasoning/content 的 token；tool_result 的结果文本

    ToolName string // tool_start / tool_result
    ToolArgs string // tool_start：原始参数 JSON

    Attempt int           // retry：第几次重试（从 1 起）
    Max     int           // retry：重试上限
    Delay   time.Duration // retry：本次重试前等待时长
    Err     error         // retry：触发重试的错误
}
```

### 各事件类型用到的字段

| Type | 有效字段 | 频率 | 说明 |
|------|---------|------|------|
| `EventReasoning` | `Text` | 高（逐 token） | **仅流式**。CoT/思考流，建议单独一个面板显示 |
| `EventContent` | `Text` | 流式高频 / 非流式一次 | 助手正文。流式是增量 token，需 UI 端累加；非流式是整段文本 |
| `EventToolStart` | `ToolName`, `ToolArgs` | 低 | 工具开始。`ToolArgs` 是原始 JSON 字符串 |
| `EventToolResult` | `ToolName`, `Text` | 低 | 工具结果。`Text` 是 JSON 串，可能是 `<persisted-output>` 预览标记（大输出已落盘） |
| `EventRetry` | `Attempt`, `Max`, `Delay`, `Err` | 低 | 重试提示，例如 "重试 2/4，1.2s 后（http 429）" |

### 顺序与并发语义（重要）

- `EventReasoning` / `EventContent`：在**流式扫描的同一 goroutine** 上按到达顺序同步发出，顺序可靠。
- `EventToolStart` / `EventToolResult`：工具是**并发执行**的，多个工具的事件会**交错**。
  对单个工具，它的 `Start` 一定先于自己的 `Result`，但中间可能夹着别的工具的事件。
  → UI 端要**按 `ToolName` 分组**展示进度。
  ⚠️ 同一工具被并行调用多次时 `ToolName` 会重名，当前事件**没有唯一调用 ID**，无法区分同名并发实例（见 §6 限制）。
- `EventRetry`：在发起重试前、`time.Sleep` 之前发出。
- 所有事件走**同一个 channel**，消费者看到的是合并后的单一事件流。

---

## 3. 并发模型（必须严格遵守）

```
┌─────────────┐  events chan   ┌──────────────┐  p.Send (异步,安全)  ┌──────────┐
│ agent goroutine │ ───────────▶ │ drain goroutine │ ──────────────────▶ │ TUI 主循环 │
│ (Agent/Stream)  │  非阻塞投递   │ (只转发,不渲染)  │                    │ (Update)  │
└─────────────┘                └──────────────┘                      └──────────┘
```

四条铁律：

1. **agent 跑在后台 goroutine**。`Agent`/`StreamAgent` 阻塞,不能在 UI 主循环里直接调。
2. **永远不要在 agent goroutine 里直接改 UI 状态**。会和渲染循环数据竞争。只能投递事件。
3. **emit 是非阻塞、满了丢弃的**（`select { case ch<-ev: default: }`）。
   所以 channel 要给**大缓冲（≥256）**，否则高频 token 会被丢。
   兜底：即便丢了几个 token 事件，**最终完整文本仍在 `Agent()` 返回的 `[]any` 里**，数据不丢，最多实时预览掉帧。
4. **用排水 goroutine 把 channel 转成 UI 框架的消息**。Bubble Tea 的 `program.Send` 是 goroutine 安全的，
   排水 goroutine 只管 `for ev := range events { p.Send(ev) }`，自己从不阻塞在渲染上。

完成信号：

```go
go func() {
    msgs, err := a.StreamAgent(input, prompt.SystemPrompt)
    p.Send(doneMsg{messages: msgs, err: err}) // 本轮结束，通知 UI
}()
```

---

## 4. Bubble Tea 参考骨架

> 放在 `cmd/tui/`。这是骨架，生成时按需扩展。

```go
package main

type doneMsg struct {
    messages []any
    err      error
}

type model struct {
    events   chan agent.AgentEvent
    program  *tea.Program
    agent    *agent.ChatCompletionAgent

    thinking string            // 累积 EventReasoning
    answer   string            // 累积 EventContent
    tools    map[string]string // ToolName -> 状态
    status   string            // 重试/错误状态行
    running  bool
}

// 把 channel 桥成 tea.Msg：每收到一个事件就交给 Update，再继续等下一个
func waitEvent(ch chan agent.AgentEvent) tea.Cmd {
    return func() tea.Msg { return <-ch }
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        if msg.Type == tea.KeyEnter && !m.running {
            m.running = true
            input := m.takeInput() // 取输入框文字
            go func() {
                msgs, err := m.agent.StreamAgent(
                    []client.Message{*cm.NewUserMessage(input)},
                    prompt.SystemPrompt,
                )
                m.program.Send(doneMsg{msgs, err})
            }()
            return m, waitEvent(m.events) // 开始监听事件
        }

    case agent.AgentEvent:
        switch msg.Type {
        case agent.EventReasoning:  m.thinking += msg.Text
        case agent.EventContent:    m.answer += msg.Text
        case agent.EventToolStart:  m.tools[msg.ToolName] = "▶ 执行中 " + msg.ToolArgs
        case agent.EventToolResult: m.tools[msg.ToolName] = "✓ 完成"
        case agent.EventRetry:
            m.status = fmt.Sprintf("⟳ 重试 %d/%d，%.1fs 后（%v）",
                msg.Attempt, msg.Max, msg.Delay.Seconds(), msg.Err)
        }
        return m, waitEvent(m.events) // 关键：再次监听下一个事件

    case doneMsg:
        m.running = false
        if msg.err != nil {
            m.status = "❌ " + msg.err.Error()
        }
        return m, nil
    }
    return m, nil
}
```

要点：每次处理完一个 `AgentEvent` 都要再返回一次 `waitEvent(m.events)`，形成「收一个→渲染→再等下一个」的循环。

---

## 5. 多轮对话怎么做

当前 API 的摩擦点：`Agent`/`StreamAgent` **入参是 `[]client.Message`，返回是 `[]any`**，
两者类型不一致，无法把上一轮返回的历史直接喂给下一轮。

建议的 TUI 侧做法（文本级历史）：

```go
var history []client.Message // TUI 自己维护
// 每轮：
history = append(history, *cm.NewUserMessage(userInput))
msgs, err := a.StreamAgent(history, prompt.SystemPrompt)
// 从事件累积的 m.answer 即本轮助手最终文本
history = append(history, *cm.NewAssistantMessage(m.answer))
```

即 TUI 只保留**纯 user/assistant 文本轮次**重新发送，工具调用历史不回传（够用，多数对话不需要把上一轮的工具结果带进下一轮）。
若确实需要完整历史回传，需要给核心加一个接受 `[]any` 的入口（小改动，按需再提）。

> 注意：上下文压缩状态（`CompactState`）、会话转录（`SessionTranscript`）都是**每次 `Agent()` 调用内部新建**的，
> 即每次调用是独立的一段会话。多轮重发历史会让转录产生多个会话文件，符合「进程级会话」的现有设计。

---

## 6. 已知限制（生成 TUI 时要规避或如实告知用户）

| 限制 | 影响 | 现状 / 规避 |
|------|------|------------|
| **无取消机制** | Agent 跑起来后**无法中途中止**（HTTP 调用没接 `context`） | "停止"按钮目前做不到真正中断；只能等本轮结束。需要的话要给核心加 `context` 支持 |
| **无工具审批钩子** | 工具**自动执行**，TUI 无法在执行前拦截/确认 | 只能事后用 `EventToolStart/Result` 展示，不能 gate |
| **同名并发工具不可区分** | 并行调用同一工具时事件 `ToolName` 重名 | UI 分组会混；当前事件无唯一调用 ID |
| **emit 满则丢弃** | 缓冲不足时高频 token 掉帧 | 用大缓冲（≥256）；最终文本仍在返回值里 |
| **非流式无 token 流** | `Agent()` 不发 `EventContent` 的增量，只在结束发整段 | 想要逐字效果就用 `StreamAgent` |
| **配置必须就位** | `~/.cc_mini_go/setting.json` 不存在则 `GetConfig` 报错 | TUI 启动时先校验配置，缺失给友好提示 |
| **流式中途断连不重试** | 已建立 200 连接后断开不会重试（避免重复输出） | 会作为错误经 `doneMsg.err` 返回，UI 提示重试由用户手动发起 |

---

## 7. 推荐的 TUI 形态（给生成用的规格）

一个最小可用的 coding-agent TUI 建议包含：

- **输入区**（底部）：多行输入框，Enter 发送，运行中禁用输入。
- **对话区**（主区，滚动）：累积渲染 `EventContent`；用户消息与助手消息分色。
- **思考区**（可折叠/侧栏）：累积渲染 `EventReasoning`，默认折叠。
- **工具进度区**：按 `ToolName` 一行行显示 `▶ 执行中` / `✓ 完成`（`EventToolStart/Result`）。
- **状态行**（顶部或底部）：显示 `EventRetry`（"⟳ 重试 2/4…"）和最终错误（`doneMsg.err`）。
- **快捷键**：Enter 发送、Ctrl+C 退出、PgUp/PgDn 滚动、Tab 切换焦点、可选 Ctrl+R 折叠思考区。

技术选型：`github.com/charmbracelet/bubbletea` +
`github.com/charmbracelet/lipgloss`（样式）+ `github.com/charmbracelet/bubbles`（输入框/视口组件）。

---

## 8. 一句话给生成器的提示

> 「按 `docs/tui-integration.md`，在 `cmd/tui/` 下用 Bubble Tea 实现一个 coding-agent TUI，
> 用 `StreamAgent` + `WithEventChannel`，实现 §7 的形态，严格遵守 §3 的并发模型，规避 §6 的限制。」
