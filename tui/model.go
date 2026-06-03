package main

import (
	"context"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/yinxiangpingfan/cc-mini-go/agent"
	"github.com/yinxiangpingfan/cc-mini-go/agent/core"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

// 输入框高度：默认 1 行，按内容增长到 maxInputHeight 行。
const (
	minInputHeight = 1
	maxInputHeight = 6
)

// entryKind 是 transcript 里一条记录的类型。
type entryKind int

const (
	entryUser entryKind = iota
	entryAssistant
	entryThinking
	entryTool
	entryTiming
	entryError
	entryNotice // 系统提示（如连续被拒的权限提示）
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
	perms   *core.PermissionEngine // 权限引擎；Shift+Tab 切换模式，帮助行展示当前模式

	input    textarea.Model
	viewport viewport.Model
	spinner  spinner.Model

	width, height int
	ready         bool

	history []any   // 多轮：完整异构历史（含工具往返），进出同型闭环回传
	entries []entry // 追加式 transcript

	// 流式过程中定位「当前正在写的块」，-1 表示需要新开一块
	curAsstIdx  int
	curThinkIdx int
	toolIdx     map[string]int // 工具调用 ID -> entries 下标（按 ID 精确配对，避免同名并发串台）

	running   bool
	cancel    context.CancelFunc
	startTime time.Time
	workWord  int // 本轮选用的 workWords 下标

	// 重试倒计时：进展事件清零 retrying；render 时按 retryUntil 实时算剩余秒
	retrying     bool
	retryUntil   time.Time
	retryAttempt int
	retryMax     int

	verbose    bool   // Ctrl+O：详细模式（显示思考 + 完整工具参数）
	killBuffer string // Ctrl+K/U 删除的文本，供 Ctrl+Y 粘贴

	pendingPerm *permissionAskMsg // 非 nil：正在等待用户对某次工具调用做 y/n 确认
}

func newModel(ag *agent.ChatCompletionAgent, cm *client.ChatCompletionMessage, events chan agent.AgentEvent, modelName string, perms *core.PermissionEngine) *model {
	ta := textarea.New()
	ta.Placeholder = `问点什么…（Enter 发送，\+Enter 或 Ctrl+J 换行）`
	ta.ShowLineNumbers = false
	ta.Prompt = "› "
	ta.CharLimit = 0
	ta.SetHeight(minInputHeight)
	// 换行不走 textarea 原生绑定：ctrl+j 与 \+Enter 都在 handleKey 里显式处理，
	// 以便「先长高再插入」，避免内部视口在矮高度下滚动顶掉首行。
	ta.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Points

	return &model{
		agent:       ag,
		cm:          cm,
		events:      events,
		model:       modelName,
		perms:       perms,
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

	case tea.MouseMsg:
		// 鼠标滚轮交给 viewport 处理（滚动 transcript）
		if m.ready {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		return m, nil

	case agent.AgentEvent:
		m.applyEvent(msg)
		m.refreshViewport()
		return m, m.waitEvent()

	case permissionAskMsg:
		// 经 program.Send 注入：进入等待确认状态，渲染 y/n 提示框。
		m.pendingPerm = &msg
		m.refreshViewport()
		return m, nil

	case doneMsg:
		m.finishTurn(msg)
		m.refreshViewport()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.running {
			m.refreshViewport() // 让底部计时 / spinner / 重试倒计时持续刷新
		}
		return m, cmd
	}

	if !m.running {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.syncInputHeight()
		return m, cmd
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// 权限确认期间：只认 y/n（及 esc/ctrl+c），吞掉其它按键，避免误输入。
	if m.pendingPerm != nil {
		return m.handlePermKey(msg)
	}

	// 粘贴（bracketed paste）：先让 textarea 插入（它会把 \r 归一成 \n），
	// 再按归一后的值算高度，并用 SetValue 把内部视口拉回顶部——否则多行粘贴
	// 会卡在内部视口下滚的位置（之前只剩末行 / 顶掉首行）。
	if msg.Paste {
		if m.running {
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		val := m.input.Value()
		m.setInputHeight(strings.Count(val, "\n") + 1)
		m.input.SetValue(val) // Reset(GotoTop)+InsertString：在新高度下重排，首行可见
		m.refreshViewport()
		return m, cmd
	}

	switch msg.String() {
	case "ctrl+c":
		// 对话中：中断当前生成；空闲时：退出应用
		if m.running {
			if m.cancel != nil {
				m.cancel()
			}
			return m, nil
		}
		return m, tea.Quit

	case "ctrl+d":
		// 退出会话（EOF）；若在生成中先取消再退出
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit

	case "ctrl+l":
		// 清屏：清空可见 transcript，保留对话历史（m.history 不动）
		m.entries = nil
		m.curAsstIdx = -1
		m.curThinkIdx = -1
		m.toolIdx = make(map[string]int)
		m.refreshViewport()
		return m, nil

	case "ctrl+o":
		// 切换详细输出（思考 + 完整工具参数）
		m.verbose = !m.verbose
		m.refreshViewport()
		return m, nil

	case "shift+tab":
		// 循环切换权限模式：auto → plan → default → auto（即时生效，下一个工具判定即用新模式）
		if m.perms != nil {
			m.perms.SetMode(cycleMode(m.perms.Mode()))
			m.refreshViewport()
		}
		return m, nil

	case "ctrl+y":
		// 粘贴 Ctrl+K/U 删除的文本
		if !m.running && m.killBuffer != "" {
			m.input.InsertString(m.killBuffer)
			m.syncInputHeight()
		}
		return m, nil

	case "ctrl+k", "ctrl+u":
		// 删除到行尾 / 整行（textarea 原生），并把删掉的文本存进 killBuffer 供 Ctrl+Y
		if m.running {
			return m, nil
		}
		before := m.input.Value()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if d := deletedChunk(before, m.input.Value()); d != "" {
			m.killBuffer = d
		}
		m.syncInputHeight()
		return m, cmd

	case "ctrl+j":
		// 换行（LF）：先按目标行数长高，再插入，避免矮高度下内部视口滚动顶掉首行
		if m.running {
			return m, nil
		}
		m.setInputHeight(strings.Count(m.input.Value(), "\n") + 2)
		m.input.InsertString("\n")
		m.refreshViewport()
		return m, nil

	case "enter":
		if m.running {
			return m, nil
		}
		// 反斜杠续行（对齐 Claude Code 的 \+Enter）：行尾是 \ 则换行继续编辑，否则发送
		if val := m.input.Value(); strings.HasSuffix(val, "\\") {
			newVal := strings.TrimSuffix(val, "\\") + "\n"
			m.setInputHeight(strings.Count(newVal, "\n") + 1) // 先长高，SetValue 后首行才不被顶掉
			m.input.SetValue(newVal)
			m.refreshViewport()
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
		m.syncInputHeight()
		return m, cmd
	}
	return m, nil
}

// handlePermKey 处理权限确认框的按键：y/Y/enter 允许，n/N/esc 拒绝，ctrl+c 拒绝并中断本轮。
func (m *model) handlePermKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		m.resolvePerm(true)
	case "n", "N", "esc":
		m.resolvePerm(false)
	case "ctrl+c":
		// 拒绝本次，并中断整轮生成
		m.resolvePerm(false)
		if m.cancel != nil {
			m.cancel()
		}
	}
	return m, nil // 确认期间吞掉其它按键
}

// resolvePerm 把用户决定回传给等待中的 approver 并退出确认状态。
// reply 带缓冲，即使 approver 已因 ctx 取消提前返回，这里发送也不会阻塞。
func (m *model) resolvePerm(ok bool) {
	if m.pendingPerm == nil {
		return
	}
	m.pendingPerm.reply <- ok
	m.pendingPerm = nil
	m.refreshViewport()
}

// deletedChunk 返回 before 相对 after 被删掉的连续片段（用于 Ctrl+K/U 后存入 killBuffer）。
// 取最长公共前缀与后缀，中间即被删除部分；适用于单段连续删除。
func deletedChunk(before, after string) string {
	if len(after) >= len(before) {
		return ""
	}
	rb, ra := []rune(before), []rune(after)
	p := 0
	for p < len(ra) && rb[p] == ra[p] {
		p++
	}
	s := 0
	for s < len(ra)-p && rb[len(rb)-1-s] == ra[len(ra)-1-s] {
		s++
	}
	return string(rb[p : len(rb)-s])
}

// syncInputHeight 让输入框高度随内容在 [min,max] 间增长，并相应重排 viewport。
func (m *model) syncInputHeight() {
	if m.setInputHeight(strings.Count(m.input.Value(), "\n") + 1) {
		m.refreshViewport()
	}
}

// setInputHeight 把输入框高度钳到 [min,max] 并设置；高度变化时重排 viewport，返回是否变化。
func (m *model) setInputHeight(lines int) bool {
	if lines < minInputHeight {
		lines = minInputHeight
	}
	if lines > maxInputHeight {
		lines = maxInputHeight
	}
	if lines == m.input.Height() {
		return false
	}
	m.input.SetHeight(lines)
	m.relayout()
	return true
}

// send 取输入框内容，开一轮后台 agent 运行。
func (m *model) send() (tea.Model, tea.Cmd) {
	text := m.input.Value()
	if len(text) == 0 {
		return m, nil
	}
	m.input.Reset()
	m.syncInputHeight() // 发送后输入框缩回 1 行

	m.entries = append(m.entries, entry{kind: entryUser, text: text})
	m.history = append(m.history, *m.cm.NewUserMessage(text))

	// 重置本轮流式定位与状态
	m.curAsstIdx = -1
	m.curThinkIdx = -1
	m.toolIdx = make(map[string]int)
	m.retrying = false
	m.running = true
	m.startTime = time.Now()
	m.workWord = rand.IntN(len(workWords))

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	history := append([]any(nil), m.history...)

	go func() {
		msgs, err := m.agent.StreamAgent(ctx, history, prompt.SystemPrompt)
		m.program.Send(doneMsg{messages: msgs, err: err})
	}()

	m.refreshViewport()
	m.viewport.GotoBottom() // 发新消息时主动滚到底，展示本轮
	return m, nil
}

// applyEvent 把一个 agent 事件并入 transcript。
func (m *model) applyEvent(ev agent.AgentEvent) {
	// 任何实际进展都意味着已重连成功，关掉重试倒计时
	switch ev.Type {
	case agent.EventReasoning, agent.EventContent, agent.EventToolStart, agent.EventToolResult:
		m.retrying = false
	}

	switch ev.Type {
	case agent.EventReasoning:
		m.curAsstIdx = -1 // 思考与正文分块
		if m.curThinkIdx < 0 {
			m.entries = append(m.entries, entry{kind: entryThinking, text: ev.Text})
			m.curThinkIdx = len(m.entries) - 1
		} else {
			m.entries[m.curThinkIdx].text += ev.Text
		}

	case agent.EventContent:
		m.curThinkIdx = -1
		if m.curAsstIdx < 0 {
			m.entries = append(m.entries, entry{kind: entryAssistant, text: ev.Text})
			m.curAsstIdx = len(m.entries) - 1
		} else {
			m.entries[m.curAsstIdx].text += ev.Text
		}

	case agent.EventToolStart:
		m.curAsstIdx = -1 // 工具后若再有正文，另起一块
		m.curThinkIdx = -1
		m.entries = append(m.entries, entry{kind: entryTool, toolName: ev.ToolName, toolArgs: ev.ToolArgs})
		m.toolIdx[ev.ToolID] = len(m.entries) - 1 // 按调用 ID 配对，避免同名并发串台

	case agent.EventToolResult:
		if idx, ok := m.toolIdx[ev.ToolID]; ok && idx < len(m.entries) {
			m.entries[idx].toolDone = true
			m.entries[idx].text = toolResultPreview(ev.Text)
		}

	case agent.EventRetry:
		// 记录倒计时基准；render 时按 retryUntil 实时算剩余秒，逐秒跳动
		m.retrying = true
		m.retryUntil = time.Now().Add(ev.Delay)
		m.retryAttempt = ev.Attempt
		m.retryMax = ev.Max

	case agent.EventPermissionHint:
		// 连续被拒提示：作为系统通知插入 transcript，并让后续正文另起一块
		m.curAsstIdx = -1
		m.curThinkIdx = -1
		m.entries = append(m.entries, entry{kind: entryNotice, text: ev.Text})
	}
}

// finishTurn 收尾一轮：提交历史、追加计时/错误条目、清状态。
func (m *model) finishTurn(msg doneMsg) {
	m.running = false
	m.cancel = nil
	m.retrying = false
	// 若仍有未决确认（如被取消时），回拒以解阻塞 approver 并退出确认态。
	if m.pendingPerm != nil {
		m.pendingPerm.reply <- false
		m.pendingPerm = nil
	}
	elapsed := int(time.Since(m.startTime).Seconds())

	// 完整历史闭环：用 agent 返回的 []any 覆盖本地历史，工具往返也随之跨轮保留。
	// 取消/出错时返回的是中断点之前的完整历史，同样直接采纳（工具往返已配对，协议安全）。
	if len(msg.messages) > 0 {
		m.history = msg.messages
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
