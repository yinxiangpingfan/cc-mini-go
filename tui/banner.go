package main

import "strings"

// bannerArt 是「MaCode」的 ANSI Shadow 风格艺术字（启动横幅）。
const bannerArt = `███╗   ███╗ █████╗  ██████╗ ██████╗ ██████╗ ███████╗
████╗ ████║██╔══██╗██╔════╝██╔═══██╗██╔══██╗██╔════╝
██╔████╔██║███████║██║     ██║   ██║██║  ██║█████╗
██║╚██╔╝██║██╔══██║██║     ██║   ██║██║  ██║██╔══╝
██║ ╚═╝ ██║██║  ██║╚██████╗╚██████╔╝██████╔╝███████╗
╚═╝     ╚═╝╚═╝  ╚═╝ ╚═════╝ ╚═════╝ ╚═════╝ ╚══════╝`

// bannerArtWidth 是艺术字最宽行的列宽。窄于此就退回紧凑横幅，避免折行错位。
const bannerArtWidth = 52

// bannerView 返回启动横幅，按终端宽度自适应：够宽用艺术字，窄则用紧凑文字版。
// 走 tea.Println 一次性刷入终端原生回滚区，置于对话之上（仿 Claude Code 启动画面）。
func bannerView(width int) string {
	var b strings.Builder
	if width >= bannerArtWidth {
		b.WriteString(bannerArtStyle.Render(bannerArt))
	} else {
		b.WriteString(bannerArtStyle.Render("✦ MaCode"))
	}
	b.WriteString("\n")
	b.WriteString(bannerSubStyle.Render("  MaCode ") +
		helpStyle.Render("· 极简 Code Agent · Chat Completion"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  Ctrl+O 详细 · Shift+Tab 权限 · Ctrl+L 清屏 · Ctrl+C 退出"))
	return b.String()
}
