package main

import (
	"context"
	"math/rand/v2"
	"time"

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

// entryKind 是 transcript 里一条记录的类型。
type entryKind int

const (
	entryUser entryKind = iota
	entryAssistant
	entryThinking
	entryTool
	entryTiming
	entryError
)

// entry 是 transcript 里追加式的一条记录（不就地销毁，整轮结束后仍保留）。
type entry struct {
	kind     entryKind
	text     string // user/assistant/thinking 文本；tool 的结果预览；timing/error 文案
	toolName string
	toolArgs string
	toolDone bool
}

// workWord 是「工作中」的拟人化措辞（致敬 Claude Code 的 Brewing/Sautéing…）。
var workWords = []struct{ ing, ed string }{
	{"Brewing", "Brewed"},
	{"Simmering", "Simmered"},
	{"Sautéing", "Sautéed"},
	{"Percolating", "Percolated"},
	{"Conjuring", "Conjured"},
	{"Noodling", "Noodled"},
	{"Marinating", "Marinated"},
	{"Whisking", "Whisked"},
	{"Pondering", "Pondered"},
	{"Tinkering", "Tinkered"},
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
	model   string

	input    textarea.Model
	viewport viewport.Model
	spinner  spinner.Model

	width, height int
	ready         bool

	history []client.Message // 多轮：纯文本 user/assistant 历史
	entries []entry          // 追加式 transcript

	// 流式过程中定位「当前正在写的块」，-1 表示需要新开一块
	curAsstIdx  int
	curThinkIdx int
	toolIdx     map[string]int // toolName -> entries 下标（同名并发会复用，属已知限制）

	turnAnswer string // 本轮 assistant 全部文本，用于提交进 history

	running   bool
	cancel    context.CancelFunc
	startTime time.Time
	workWord  int    // 本轮选用的 workWords 下标
	retry     string // 当前重试提示；任何进展事件都会清空（修复粘住 bug）

	showThink bool // 是否展开思考块
}

func newModel(ag *agent.ChatCompletionAgent, cm *client.ChatCompletionMessage, events chan agent.AgentEvent, modelName string) *model {
	ta := textarea.New()
	ta.Placeholder = "问点什么…（Enter 发送，Ctrl+J 换行）"
	ta.ShowLineNumbers = false
	ta.Prompt = "› "
	ta.CharLimit = 0
	ta.SetHeight(inputHeight)
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j"))
	ta.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Points

	return &model{
		agent:       ag,
		cm:          cm,
		events:      events,
		model:       modelName,
		input:       ta,
		spinner:     sp,
		curAsstIdx:  -1,
		curThinkIdx: -1,
		toolIdx:     make(map[string]int),
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spinner.Tick, m.waitEvent())
}

// waitEvent 把事件 channel 桥成 tea.Msg；全程仅保留一个监听者，保证事件顺序。
func (m *model) waitEvent() tea.Cmd {
	return func() tea.Msg { return <-m.events }
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case agent.AgentEvent:
		m.applyEvent(msg)
		m.refreshViewport()
		return m, m.waitEvent()

	case doneMsg:
		m.finishTurn(msg)
		m.refreshViewport()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.running {
			m.refreshViewport() // 让底部计时 / spinner 持续刷新
		}
		return m, cmd
	}

	if !m.running {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.running {
			if m.cancel != nil {
				m.cancel() // 停止当前轮：中断 LLM 与可取消工具
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

	m.entries = append(m.entries, entry{kind: entryUser, text: text})
	m.history = append(m.history, *m.cm.NewUserMessage(text))

	// 重置本轮流式定位与状态
	m.turnAnswer = ""
	m.curAsstIdx = -1
	m.curThinkIdx = -1
	m.toolIdx = make(map[string]int)
	m.retry = ""
	m.running = true
	m.startTime = time.Now()
	m.workWord = rand.IntN(len(workWords))

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	history := append([]client.Message(nil), m.history...)

	go func() {
		msgs, err := m.agent.StreamAgent(ctx, history, prompt.SystemPrompt)
		m.program.Send(doneMsg{messages: msgs, err: err})
	}()

	m.refreshViewport()
	return m, nil
}

// applyEvent 把一个 agent 事件并入 transcript。
func (m *model) applyEvent(ev agent.AgentEvent) {
	switch ev.Type {
	case agent.EventReasoning:
		m.retry = ""
		m.curAsstIdx = -1 // 思考与正文分块
		if m.curThinkIdx < 0 {
			m.entries = append(m.entries, entry{kind: entryThinking, text: ev.Text})
			m.curThinkIdx = len(m.entries) - 1
		} else {
			m.entries[m.curThinkIdx].text += ev.Text
		}

	case agent.EventContent:
		m.retry = ""
		m.curThinkIdx = -1
		m.turnAnswer += ev.Text
		if m.curAsstIdx < 0 {
			m.entries = append(m.entries, entry{kind: entryAssistant, text: ev.Text})
			m.curAsstIdx = len(m.entries) - 1
		} else {
			m.entries[m.curAsstIdx].text += ev.Text
		}

	case agent.EventToolStart:
		m.retry = ""
		m.curAsstIdx = -1 // 工具后若再有正文，另起一块
		m.curThinkIdx = -1
		m.entries = append(m.entries, entry{kind: entryTool, toolName: ev.ToolName, toolArgs: ev.ToolArgs})
		m.toolIdx[ev.ToolName] = len(m.entries) - 1

	case agent.EventToolResult:
		m.retry = ""
		if idx, ok := m.toolIdx[ev.ToolName]; ok && idx < len(m.entries) {
			m.entries[idx].toolDone = true
			m.entries[idx].text = toolResultPreview(ev.Text)
		}

	case agent.EventRetry:
		// 只更新「当前工作行」，不固化成永久条目；下一个进展事件会清掉它
		m.retry = retryLine(ev)
	}
}

// finishTurn 收尾一轮：提交历史、追加计时/错误条目、清状态。
func (m *model) finishTurn(msg doneMsg) {
	m.running = false
	m.cancel = nil
	m.retry = ""
	elapsed := int(time.Since(m.startTime).Seconds())

	if m.turnAnswer != "" {
		m.history = append(m.history, *m.cm.NewAssistantMessage(m.turnAnswer))
	}

	switch {
	case msg.err != nil && isCanceled(msg.err):
		m.entries = append(m.entries, entry{kind: entryError, text: "已中断"})
	case msg.err != nil:
		m.entries = append(m.entries, entry{kind: entryError, text: msg.err.Error()})
	default:
		m.entries = append(m.entries, entry{
			kind: entryTiming,
			text: workWords[m.workWord].ed + " for " + secs(elapsed),
		})
	}
}
