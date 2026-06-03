package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// resize 根据终端尺寸重排：transcript 占主区，输入框 + 帮助行固定在底部。
func (m *model) resize(w, h int) {
	m.width, m.height = w, h
	if !m.ready {
		m.viewport = viewport.New(w, 1)
		m.ready = true
	}
	m.relayout()
	m.refreshViewport()
}

// relayout 按当前输入框高度重算 viewport 尺寸（输入框增高时调用）。
func (m *model) relayout() {
	if !m.ready {
		return
	}
	// 垂直预算：输入块(input 高度 + 边框2) + 帮助行(1)
	vpHeight := m.height - (m.input.Height() + 2) - 1
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.viewport.Width = m.width
	m.viewport.Height = vpHeight
	m.input.SetWidth(m.width - 2)
}

// refreshViewport 重建 transcript 内容；仅当用户本就停在底部时才跟随到底，
// 这样生成中向上滚动查看历史不会被强行拽回（修复「一直聚焦最下方」）。
func (m *model) refreshViewport() {
	if !m.ready {
		return
	}
	atBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.renderTranscript(m.viewport.Width))
	if atBottom {
		m.viewport.GotoBottom()
	}
}

// renderTranscript 把所有条目 + 底部工作行渲染成可滚动文本。
func (m *model) renderTranscript(width int) string {
	textWidth := width - 2 // 给行首的 2 字符标记（"● " / "› "）留位，避免溢出
	if textWidth < 1 {
		textWidth = 1
	}
	wrap := lipgloss.NewStyle().Width(textWidth)
	indent := lipgloss.NewStyle().Width(textWidth)
	var b strings.Builder

	for _, e := range m.entries {
		switch e.kind {
		case entryUser:
			b.WriteString(userLabelStyle.Render("› ") + wrap.Render(e.text))
			b.WriteString("\n\n")

		case entryAssistant:
			b.WriteString(agentLabelStyle.Render("● ") + wrap.Render(e.text))
			b.WriteString("\n\n")

		case entryThinking:
			if m.verbose {
				b.WriteString(thinkStyle.Render("✶ thinking"))
				b.WriteString("\n")
				b.WriteString(indent.Render(thinkStyle.Render(e.text)))
				b.WriteString("\n\n")
			}

		case entryTool:
			mark := toolRunningStyle.Render("●")
			if e.toolDone {
				mark = toolDoneStyle.Render("●")
			}
			// 详细模式下展示更完整的参数
			argLimit := 72
			if m.verbose {
				argLimit = 200
			}
			b.WriteString(mark + " " + toolStyle.Render(toolDescriptor(e.toolName, e.toolArgs, argLimit)))
			b.WriteString("\n")
			if e.toolDone && e.text != "" {
				b.WriteString(helpStyle.Render("  ⎿ " + e.text))
				b.WriteString("\n")
			}
			b.WriteString("\n")

		case entryTiming:
			b.WriteString(timingStyle.Render("✲ " + e.text))
			b.WriteString("\n\n")

		case entryError:
			b.WriteString(errStyle.Render("✗ " + e.text))
			b.WriteString("\n\n")

		case entryNotice:
			b.WriteString(permTitleStyle.Render("⚠ " + e.text))
			b.WriteString("\n\n")
		}
	}

	// 底部工作行：重试倒计时优先，否则显示拟人化计时 spinner
	if m.running {
		if m.retrying {
			b.WriteString(m.spinner.View() + " " + retryStyle.Render(m.retryCountdown()))
		} else {
			elapsed := int(time.Since(m.startTime).Seconds())
			label := fmt.Sprintf("%s… (%s · ctrl+c 中断)", workWords[m.workWord].ing, secs(elapsed))
			b.WriteString(m.spinner.View() + " " + workStyle.Render(label))
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func (m *model) View() string {
	if !m.ready {
		return "正在初始化…"
	}
	// 等待权限确认时，用 y/n 提示框取代输入区。
	if m.pendingPerm != nil {
		return strings.Join([]string{
			m.viewport.View(),
			m.permPromptView(),
		}, "\n")
	}
	help := helpStyle.Render(m.helpLine())
	return strings.Join([]string{
		m.viewport.View(),
		inputBorderStyle.Render(m.input.View()),
		help,
	}, "\n")
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
		return mode + "ctrl+c 中断 · shift+tab 模式 · ctrl+o 详细 · 滚轮 滚动"
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
