package main

import "github.com/charmbracelet/lipgloss"

// 集中管理界面配色与样式，便于统一调整外观。
var (
	colorUser   = lipgloss.Color("12")  // 亮蓝：用户消息
	colorAgent  = lipgloss.Color("15")  // 亮白：助手消息
	colorThink  = lipgloss.Color("244") // 灰：思考流
	colorTool   = lipgloss.Color("13")  // 品红：工具
	colorErr    = lipgloss.Color("9")   // 红：错误
	colorRetry  = lipgloss.Color("11")  // 黄：重试
	colorSubtle = lipgloss.Color("240") // 暗灰：边框/提示
	colorAccent = lipgloss.Color("14")  // 青：强调
	colorDone   = lipgloss.Color("10")  // 绿：工具完成

	userLabelStyle  = lipgloss.NewStyle().Foreground(colorUser).Bold(true)
	agentLabelStyle = lipgloss.NewStyle().Foreground(colorAgent).Bold(true)
	thinkStyle      = lipgloss.NewStyle().Foreground(colorThink).Italic(true)

	toolStyle        = lipgloss.NewStyle().Foreground(colorTool)
	toolRunningStyle = lipgloss.NewStyle().Foreground(colorRetry) // 运行中：黄点
	toolDoneStyle    = lipgloss.NewStyle().Foreground(colorDone)  // 完成：绿点

	timingStyle = lipgloss.NewStyle().Foreground(colorSubtle).Italic(true)
	workStyle   = lipgloss.NewStyle().Foreground(colorAccent)
	errStyle    = lipgloss.NewStyle().Foreground(colorErr).Bold(true)
	retryStyle  = lipgloss.NewStyle().Foreground(colorRetry)

	helpStyle = lipgloss.NewStyle().Foreground(colorSubtle)

	inputBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorSubtle)

	// 权限确认框：黄色边框，醒目地把待确认的工具调用与按键提示框起来。
	permBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorRetry).
			Padding(0, 1)
	permTitleStyle = lipgloss.NewStyle().Foreground(colorRetry).Bold(true)
	permKeyStyle   = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

	// 计划面板：青色边框常驻输入框上方，按状态分色显示任务。
	planBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(0, 1)
	planTitleStyle   = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	planDoneStyle    = lipgloss.NewStyle().Foreground(colorDone).Strikethrough(true) // 完成：绿+删除线
	planActiveStyle  = lipgloss.NewStyle().Foreground(colorRetry).Bold(true)         // 进行中：黄+加粗
	planPendingStyle = lipgloss.NewStyle().Foreground(colorSubtle)                   // 待办：暗灰

	// 启动横幅：MaCode 艺术字用青色加粗，副标题用亮白加粗。
	bannerArtStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	bannerSubStyle = lipgloss.NewStyle().Foreground(colorAgent).Bold(true)
)
