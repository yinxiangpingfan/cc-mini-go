package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// newViewport 创建一个带初始尺寸的滚动视口。
func newViewport(width, height int) viewport.Model {
	vp := viewport.New(width, height)
	return vp
}

// resize 根据终端尺寸重排各区域。首次收到尺寸时创建 viewport。
func (m *model) resize(w, h int) {
	m.width, m.height = w, h

	innerWidth := w - 2 // 减去 viewport 边框
	if innerWidth < 1 {
		innerWidth = 1
	}
	// 垂直预算：标题(1) + 状态(1) + 帮助(1) + 输入块(inputHeight+边框2) + viewport 边框(2)
	vpHeight := h - 1 - 1 - 1 - (inputHeight + 2) - 2
	if vpHeight < 1 {
		vpHeight = 1
	}

	if !m.ready {
		m.viewport = newViewport(innerWidth, vpHeight)
		m.ready = true
	} else {
		m.viewport.Width = innerWidth
		m.viewport.Height = vpHeight
	}

	m.input.SetWidth(w - 2)
	m.refreshViewport()
}

// refreshViewport 重建对话内容并在运行中自动滚到底部。
func (m *model) refreshViewport() {
	if !m.ready {
		return
	}
	atBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.renderContent(m.viewport.Width))
	if m.running || atBottom {
		m.viewport.GotoBottom()
	}
}

// renderContent 把已完成轮次 + 当前实时态拼成可滚动文本。
func (m *model) renderContent(width int) string {
	wrap := lipgloss.NewStyle().Width(width)
	var b strings.Builder

	writeTurn := func(t turn) {
		switch t.role {
		case "user":
			b.WriteString(userLabelStyle.Render("› 你"))
		case "agent":
			b.WriteString(agentLabelStyle.Render("● 助手"))
		}
		b.WriteString("\n")
		b.WriteString(wrap.Render(t.text))
		b.WriteString("\n\n")
	}

	for _, t := range m.turns {
		writeTurn(t)
	}

	if m.running {
		// 思考面板（可折叠）
		if m.showThink && m.thinking != "" {
			b.WriteString(thinkStyle.Render("✎ 思考（Ctrl+T 折叠）"))
			b.WriteString("\n")
			b.WriteString(wrap.Render(thinkStyle.Render(m.thinking)))
			b.WriteString("\n\n")
		}
		// 工具进度
		if len(m.toolOrder) > 0 {
			for _, name := range m.toolOrder {
				b.WriteString(m.tools[name])
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
		// 实时助手文本
		b.WriteString(agentLabelStyle.Render("● 助手"))
		b.WriteString("\n")
		if m.answer != "" {
			b.WriteString(wrap.Render(m.answer))
		} else {
			b.WriteString(helpStyle.Render(m.spinner.View() + " 思考中…"))
		}
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

func (m *model) View() string {
	if !m.ready {
		return "正在初始化…"
	}

	header := titleStyle.Render(" cc-mini-go ") + " " +
		helpStyle.Render(m.model)

	// 状态行：运行中显示 spinner，否则显示重试/错误状态（可能为空）。
	var status string
	switch {
	case m.running && m.status != "":
		status = m.spinner.View() + " " + m.status
	case m.running:
		status = m.spinner.View() + " " + helpStyle.Render("运行中… Ctrl+C 停止")
	default:
		status = m.status
	}

	help := helpStyle.Render(m.helpLine())

	return strings.Join([]string{
		header,
		panelBorderStyle.Render(m.viewport.View()),
		status,
		inputBorderStyle.Render(m.input.View()),
		help,
	}, "\n")
}

func (m *model) helpLine() string {
	if m.running {
		return "Ctrl+C 停止 · Ctrl+T 思考 · PgUp/PgDn 滚动"
	}
	return "Enter 发送 · Ctrl+J 换行 · Ctrl+T 思考 · PgUp/PgDn 滚动 · Ctrl+C 退出"
}
