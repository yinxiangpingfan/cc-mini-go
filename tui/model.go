package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/yinxiangpingfan/cc-mini-go/agent"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

// inputHeight 输入框固定行数。
const inputHeight = 3

// turn 是一轮已完成的对话（用于渲染）。
type turn struct {
	role string // "user" / "agent"
	text string
}

// doneMsg 一轮 agent 运行结束（含被取消）后投递给 UI。
type doneMsg struct {
	messages []any
	err      error
}

type model struct {
	program *tea.Program
	agent   *agent.ChatCompletionAgent
	cm      *client.ChatCompletionMessage
	events  chan agent.AgentEvent
	model   string // 模型名，仅用于标题展示

	input    textarea.Model
	viewport viewport.Model
	spinner  spinner.Model

	width, height int
	ready         bool

	history   []client.Message  // 多轮：只回传纯文本 user/assistant（见 docs §5）
	turns     []turn            // 已完成轮次
	answer    string            // 当前流式累积的助手文本
	thinking  string            // 当前流式累积的思考
	tools     map[string]string // toolName -> 渲染好的状态行
	toolOrder []string          // 工具出现顺序，稳定渲染

	running   bool
	cancel    context.CancelFunc
	status    string // 重试 / 错误状态行
	showThink bool   // 是否展开思考面板
}

func newModel(ag *agent.ChatCompletionAgent, cm *client.ChatCompletionMessage, events chan agent.AgentEvent, modelName string) *model {
	ta := textarea.New()
	ta.Placeholder = "输入消息，Enter 发送，Ctrl+J 换行…"
	ta.ShowLineNumbers = false
	ta.Prompt = "┃ "
	ta.CharLimit = 0
	ta.SetHeight(inputHeight)
	// Enter 留给「发送」（在 handleKey 拦截），换行改绑 Ctrl+J。
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j"))
	ta.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	return &model{
		agent:     ag,
		cm:        cm,
		events:    events,
		model:     modelName,
		input:     ta,
		spinner:   sp,
		tools:     make(map[string]string),
		showThink: false,
	}
}

func (m *model) Init() tea.Cmd {
	// 一次性启动事件监听器（全程仅保留一个，避免并发消费打乱顺序）。
	return tea.Batch(textarea.Blink, m.spinner.Tick, m.waitEvent())
}

// waitEvent 把事件 channel 桥成 tea.Msg。每收到一个事件，Update 里再发一次，形成单一监听循环。
func (m *model) waitEvent() tea.Cmd {
	return func() tea.Msg { return <-m.events }
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case agent.AgentEvent:
		m.applyEvent(msg)
		m.refreshViewport()
		// 关键：再次监听下一个事件。
		return m, m.waitEvent()

	case doneMsg:
		m.finishTurn(msg)
		m.refreshViewport()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	// 其余消息交给输入框（光标闪烁等）。
	if !m.running {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.running {
			// 停止当前轮：中断进行中的 LLM 请求与可取消工具（StreamAgent 很快返回 context.Canceled）。
			if m.cancel != nil {
				m.cancel()
			}
			return m, nil
		}
		return m, tea.Quit

	case "ctrl+t":
		m.showThink = !m.showThink
		m.refreshViewport()
		return m, nil

	case "enter":
		if m.running {
			return m, nil
		}
		return m.send()

	case "pgup":
		m.viewport.HalfPageUp()
		return m, nil
	case "pgdown":
		m.viewport.HalfPageDown()
		return m, nil
	}

	// 其它按键（含 Ctrl+J 换行）交给输入框，运行中禁用输入。
	if !m.running {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// send 取输入框内容，开一轮后台 agent 运行。
func (m *model) send() (tea.Model, tea.Cmd) {
	text := m.input.Value()
	if len(text) == 0 {
		return m, nil
	}
	m.input.Reset()

	// 记录用户轮次并加入回传历史。
	m.turns = append(m.turns, turn{role: "user", text: text})
	m.history = append(m.history, *m.cm.NewUserMessage(text))

	// 清空本轮实时态。
	m.answer = ""
	m.thinking = ""
	m.tools = make(map[string]string)
	m.toolOrder = nil
	m.status = ""
	m.running = true

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	history := append([]client.Message(nil), m.history...) // 拷贝快照，避免与后续轮共享底层数组

	go func() {
		msgs, err := m.agent.StreamAgent(ctx, history, prompt.SystemPrompt)
		m.program.Send(doneMsg{messages: msgs, err: err})
	}()

	m.refreshViewport()
	// 不重新启动 spinner：tick 循环自 Init 起已自持（见 spinner.TickMsg 分支）。
	return m, nil
}

// applyEvent 把一个 agent 事件并入 UI 状态。
func (m *model) applyEvent(ev agent.AgentEvent) {
	switch ev.Type {
	case agent.EventReasoning:
		m.thinking += ev.Text
	case agent.EventContent:
		m.answer += ev.Text
	case agent.EventToolStart:
		if _, seen := m.tools[ev.ToolName]; !seen {
			m.toolOrder = append(m.toolOrder, ev.ToolName)
		}
		m.tools[ev.ToolName] = toolRunningStyle.Render("▶ "+ev.ToolName) + helpStyle.Render(" "+truncate(ev.ToolArgs, 60))
	case agent.EventToolResult:
		if _, seen := m.tools[ev.ToolName]; !seen {
			m.toolOrder = append(m.toolOrder, ev.ToolName)
		}
		m.tools[ev.ToolName] = toolDoneStyle.Render("✓ " + ev.ToolName)
	case agent.EventRetry:
		m.status = retryStyle.Render(fmt.Sprintf("⟳ 重试 %d/%d，%.1fs 后（%v）",
			ev.Attempt, ev.Max, ev.Delay.Seconds(), ev.Err))
	}
}

// finishTurn 收尾一轮：把助手文本固化进 turns 与回传历史。
func (m *model) finishTurn(msg doneMsg) {
	m.running = false
	m.cancel = nil

	if msg.err != nil {
		if m.answer != "" {
			m.turns = append(m.turns, turn{role: "agent", text: m.answer})
			m.history = append(m.history, *m.cm.NewAssistantMessage(m.answer))
		}
		m.status = errStyle.Render("❌ " + msg.err.Error())
		m.answer = ""
		return
	}

	if m.answer != "" {
		m.turns = append(m.turns, turn{role: "agent", text: m.answer})
		m.history = append(m.history, *m.cm.NewAssistantMessage(m.answer))
	}
	m.answer = ""
	m.status = ""
}

// truncate 截断过长字符串，避免工具参数撑爆一行。
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
