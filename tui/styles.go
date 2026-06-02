package main

import "github.com/charmbracelet/lipgloss"

// 集中管理界面配色与样式，便于统一调整外观。
var (
	colorUser    = lipgloss.Color("12")  // 亮蓝：用户消息
	colorAgent   = lipgloss.Color("10")  // 亮绿：助手消息
	colorThink   = lipgloss.Color("244") // 灰：思考流
	colorTool    = lipgloss.Color("13")  // 品红：工具
	colorErr     = lipgloss.Color("9")   // 红：错误
	colorRetry   = lipgloss.Color("11")  // 黄：重试
	colorSubtle  = lipgloss.Color("240") // 暗灰：边框/提示
	colorAccent  = lipgloss.Color("14")  // 青：标题
	colorSuccess = lipgloss.Color("10")  // 绿：完成

	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("0")).
			Background(colorAccent).
			Bold(true).
			Padding(0, 1)

	userLabelStyle  = lipgloss.NewStyle().Foreground(colorUser).Bold(true)
	agentLabelStyle = lipgloss.NewStyle().Foreground(colorAgent).Bold(true)
	thinkStyle      = lipgloss.NewStyle().Foreground(colorThink).Italic(true)

	toolRunningStyle = lipgloss.NewStyle().Foreground(colorTool)
	toolDoneStyle    = lipgloss.NewStyle().Foreground(colorSuccess)

	errStyle   = lipgloss.NewStyle().Foreground(colorErr).Bold(true)
	retryStyle = lipgloss.NewStyle().Foreground(colorRetry)

	helpStyle = lipgloss.NewStyle().Foreground(colorSubtle)

	panelBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorSubtle)

	inputBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorAccent)
)
