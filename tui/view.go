package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// resize 记录终端尺寸并调整输入框宽度。内联渲染下没有 viewport，布局由 View 每帧重算。
func (m *model) resize(w, h int) {
	m.width, m.height = w, h
	m.ready = true
	m.input.SetWidth(w - 2)
}

// relayout 旧 viewport 布局的遗留入口；内联渲染下无需重算尺寸，留作空操作以兼容调用点。
func (m *model) relayout() {}

// refreshViewport 旧 viewport 的内容刷新入口；内联渲染下 View 每次 Update 后自动重算，空操作。
func (m *model) refreshViewport() {}

// renderEntry 把单个条目渲染成一个文本块（不含尾随换行）。返回 "" 表示该条目应跳过。
// 助手正文 markdown 渲染策略：final（刷入回滚区）强制渲染；live 区里「正在写的那条」(active)
// 按节流 markdown 实时渲染（≤8/s，不拖慢事件消费）；其余已完成块先用原文，待 finishTurn 统一渲染。
func (m *model) renderEntry(e entry, width int, final, active bool) string {
	textWidth := width - 2 // 给行首的 2 字符标记（"● " / "› "）留位
	if textWidth < 1 {
		textWidth = 1
	}
	switch e.kind {
	case entryUser:
		return userLabelStyle.Render("› ") + lipgloss.NewStyle().Width(textWidth).Render(e.text)
	case entryAssistant:
		txt := strings.TrimSpace(e.text)
		if txt == "" {
			return "" // 跳过只有空白的助手块（避免孤零零的圆点）
		}
		if final || active {
			md := renderMarkdown(txt, textWidth, final)
			if md == "" {
				return ""
			}
			if m.asstMarkerShown {
				return md // 圆点已随首段刷入回滚区：续写不重复「●」
			}
			return agentLabelStyle.Render("●") + "\n" + md
		}
		return agentLabelStyle.Render("● ") + lipgloss.NewStyle().Width(textWidth).Render(txt)
	case entryThinking:
		txt := strings.TrimSpace(e.text)
		if !m.verbose || txt == "" {
			return ""
		}
		return thinkStyle.Render("✶ 思考") + "\n" + quoteBlock(txt, textWidth, thinkStyle)
	case entryTool:
		mark := toolRunningStyle.Render("●")
		if e.toolDone {
			mark = toolDoneStyle.Render("●")
		}
		argLimit := 72 // 详细模式下展示更完整的参数
		if m.verbose {
			argLimit = 200
		}
		desc := toolDescriptor(e.toolName, e.toolArgs, argLimit)
		s := mark + " " + toolStyle.Render(clampCells(desc, textWidth))
		if e.toolDone && e.text != "" {
			s += "\n" + helpStyle.Render(clampCells("  ⎿ "+e.text, width))
		}
		return s
	case entryTiming:
		return timingStyle.Render(clampCells("✲ "+e.text, width))
	case entryError:
		return errStyle.Render(clampCells("✗ "+e.text, width))
	case entryNotice:
		return permTitleStyle.Render(clampCells("⚠ "+e.text, width))
	}
	return ""
}

// renderAsstChunk 把一段已完成的助手正文按 markdown 渲染（force，准确），供逐块刷入回滚区。
// 空白返回 ""；首段带「●」，marker 已显示则续写省略。
func (m *model) renderAsstChunk(text string, width int) string {
	textWidth := width - 2
	if textWidth < 1 {
		textWidth = 1
	}
	md := renderMarkdown(strings.TrimSpace(text), textWidth, true)
	if md == "" {
		return ""
	}
	if m.asstMarkerShown {
		return md
	}
	return agentLabelStyle.Render("●") + "\n" + md
}

// splitFlushable 把流式助手正文切成「可安全刷出的完整部分 head」与「仍在写的尾巴 tail」。
// 边界是「代码块之外的空行」：完整段落 / 闭合代码块才刷，未闭合的代码块整体留在 tail。
func splitFlushable(text string) (head, tail string) {
	lines := strings.Split(text, "\n")
	inFence := false
	lastBoundary := -1
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "```") {
			inFence = !inFence
			continue
		}
		if !inFence && t == "" {
			lastBoundary = i
		}
	}
	if lastBoundary < 0 {
		return "", text
	}
	return strings.Join(lines[:lastBoundary], "\n"), strings.Join(lines[lastBoundary+1:], "\n")
}

// renderTranscript 把当前条目 + 底部工作行渲染成文本。final 见 renderEntry。
func (m *model) renderTranscript(width int, final bool) string {
	var blocks []string
	for i, e := range m.entries {
		active := !final && i == m.curAsstIdx // 正在写的那条助手消息：live markdown
		if s := m.renderEntry(e, width, final, active); s != "" {
			blocks = append(blocks, s)
		}
	}
	body := strings.Join(blocks, "\n")

	// 底部工作行：重试倒计时优先，否则显示拟人化计时 spinner
	if m.running {
		var work string
		if m.retrying {
			work = m.spinner.View() + " " + retryStyle.Render(m.retryCountdown())
		} else {
			elapsed := int(time.Since(m.startTime).Seconds())
			label := fmt.Sprintf("%s… (%s · ctrl+c 中断)", workWords[m.workWord].ing, secs(elapsed))
			work = m.spinner.View() + " " + workStyle.Render(label)
		}
		if body != "" {
			body += "\n"
		}
		body += work
	}
	return body
}

func (m *model) View() string {
	if !m.ready {
		return "正在初始化…"
	}
	// 先组装底部固定区（计划面板 + 输入/权限 + 帮助）。它的高度决定上方 live transcript 能占多少行。
	var bottom []string
	if panel := m.planPanelView(); panel != "" {
		bottom = append(bottom, panel)
	}
	if m.pendingPerm != nil {
		bottom = append(bottom, m.permPromptView())
	} else {
		bottom = append(bottom,
			inputBorderStyle.Render(m.input.View()),
			// 限制到终端宽度：帮助行过长会折行，打乱内联渲染器的行数统计、缩放时残留多个输入框。
			helpStyle.MaxWidth(m.width).Render(m.helpLine()),
		)
	}
	bottomStr := strings.Join(bottom, "\n")

	// live transcript 占剩余高度；超出就只保留尾部（最新内容）。
	// 这既避免 live 区高于屏幕、破坏内联渲染（缩放残留 / 长输出错位），也保证流式时总能看到最新进展。
	// 已结束的轮次已 Println 进终端原生回滚区，可原生上下滚动 / 框选复制，不受此截断影响。
	body := m.renderTranscript(m.width, false) // live 区渲染原文（快），markdown 留到刷入回滚区
	if body != "" {
		body = tailLines(body, m.height-lipgloss.Height(bottomStr)-1)
	}
	if body == "" {
		return bottomStr
	}
	return body + "\n" + bottomStr
}

// tailLines 只保留 s 的最后 n 行（n<1 视为 1），把 live 区限制在可见高度内。
func tailLines(s string, n int) string {
	if n < 1 {
		n = 1
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// planMarkers 把任务状态映射成清单标记（对齐 agent_tools.statusMarkers）。
var planMarkers = map[string]string{
	"pending":     "[ ]",
	"in_progress": "[>]",
	"completed":   "[x]",
}

// planStyleFor 按任务状态选配色。
func planStyleFor(status string) lipgloss.Style {
	switch status {
	case "completed":
		return planDoneStyle
	case "in_progress":
		return planActiveStyle
	default:
		return planPendingStyle
	}
}

// planPanelView 渲染计划面板：标题 + 每个任务一行（按状态分色）。无活动计划时返回空串。
func (m *model) planPanelView() string {
	if !m.hasActivePlan() {
		return ""
	}
	innerW := m.width - 4 // 边框 2 + 左右 padding 2，留给文本的宽度
	if innerW < 1 {
		innerW = 1
	}
	var b strings.Builder
	b.WriteString(planTitleStyle.Render("Plan"))
	for _, it := range m.plan {
		marker, ok := planMarkers[it.Status]
		if !ok {
			marker = "[ ]"
		}
		line := truncate(oneLine(marker+" "+it.Subject), innerW) // 截断防换行，保证行数与 height 一致
		b.WriteString("\n" + planStyleFor(it.Status).Render(line))
	}
	return planBorderStyle.Width(m.width - 2).Render(b.String())
}

// permPromptView 渲染权限确认框：待确认的工具调用 + 原因 + 按键提示。
func (m *model) permPromptView() string {
	req := m.pendingPerm.req
	desc := toolDescriptor(req.ToolName, req.RawArgs, 200)

	var b strings.Builder
	b.WriteString(permTitleStyle.Render("⚠ 需要确认") + "  " + toolStyle.Render(desc))
	if req.Reason != "" {
		b.WriteString("\n" + helpStyle.Render(req.Reason))
	}
	b.WriteString("\n" +
		permKeyStyle.Render("y") + " 允许   " +
		permKeyStyle.Render("n") + " 拒绝   " +
		helpStyle.Render("esc 拒绝 · ctrl+c 中断本轮"))
	return permBorderStyle.Width(m.width - 2).Render(b.String())
}

func (m *model) helpLine() string {
	mode := ""
	if m.perms != nil {
		mode = "[" + modeLabel(m.perms.Mode()) + "] "
	}
	if m.running {
		return mode + "ctrl+c 中断 · shift+tab 模式 · ctrl+o 详细"
	}
	return mode + `enter 发送 · \+enter 换行 · shift+tab 模式 · ctrl+o 详细 · ctrl+l 清屏 · ctrl+c 退出`
}

// ---- 渲染辅助 ----

// toolDescriptor 把工具名 + 原始参数 JSON 压成一行可读描述，如 bash(go build ./...)。
func toolDescriptor(name, argsJSON string, limit int) string {
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)

	key := ""
	switch name {
	case "bash":
		key, _ = args["command"].(string)
	case "read_file", "write_file", "edit_file":
		key, _ = args["file_path"].(string)
	case "grep", "glob":
		key, _ = args["pattern"].(string)
	case "load_skill":
		key, _ = args["name"].(string)
	case "task":
		key, _ = args["description"].(string)
	}
	if key == "" {
		key = strings.TrimSpace(argsJSON)
	}
	key = strings.ReplaceAll(key, "\n", " ")
	if key == "" || key == "{}" {
		return name
	}
	return fmt.Sprintf("%s(%s)", name, truncate(key, limit))
}

// toolResultPreview 把工具结果 JSON 压成一行预览：错误高亮，正文取首行。
func toolResultPreview(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "<persisted-output>") {
		return "(大输出已落盘)"
	}
	var obj map[string]any
	if json.Unmarshal([]byte(s), &obj) == nil {
		if e, ok := obj["error"].(string); ok {
			return "error: " + oneLine(e)
		}
		if c, ok := obj["content"].(string); ok {
			return oneLine(c)
		}
	}
	return oneLine(s)
}

// retryCountdown 按 retryUntil 实时算剩余秒，做成逐秒跳动的倒计时；归零后显示「重连中…」。
func (m *model) retryCountdown() string {
	remain := time.Until(m.retryUntil)
	if remain <= 0 {
		return fmt.Sprintf("重试 %d/%d，重连中…", m.retryAttempt, m.retryMax)
	}
	// 向上取整：剩 2.3s 显示 3s，逐秒 3→2→1
	left := int((remain + time.Second - 1) / time.Second)
	return fmt.Sprintf("重试 %d/%d，%s 后重连", m.retryAttempt, m.retryMax, secs(left))
}

// quoteBlock 把文本按 width 换行，每行前加一道暗色竖线，整体用 style 着色——用于思考块等次要内容。
func quoteBlock(text string, width int, style lipgloss.Style) string {
	inner := width - 2 // 留给 "▏ " 前缀
	if inner < 1 {
		inner = 1
	}
	wrapped := lipgloss.NewStyle().Width(inner).Render(text)
	var b strings.Builder
	for i, ln := range strings.Split(strings.TrimRight(wrapped, "\n"), "\n") {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(style.Render("▏ " + ln))
	}
	return b.String()
}

// clampCells 把文本按显示宽度（列）硬截断到 w 列，多字节/CJK 安全。
// 用于单行摘要，防止超宽行被终端折行、进而打乱内联渲染器的行数统计（缩放时残留多个输入框）。
func clampCells(s string, w int) string {
	if w < 1 {
		w = 1
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return truncate(s, 100)
}

// truncate 按 rune 截断，避免切断多字节字符。
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

func secs(n int) string {
	if n == 1 {
		return "1s"
	}
	return fmt.Sprintf("%ds", n)
}

func isCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
