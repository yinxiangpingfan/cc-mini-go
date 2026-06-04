package main

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
)

// mdThrottle 是流式途中两次真实 glamour 渲染的最小间隔。token 来得飞快（每秒几十上百），
// 但 glamour 每次 ~数毫秒——若每 token 都渲染，会拖慢事件消费、撑爆 channel 导致丢字。
// 故对「非强制」渲染做时间节流（约 8/s），强制渲染（刷入回滚区时）不受限。
const mdThrottle = 120 * time.Millisecond

// trailingPad 匹配行尾的「纯空白 + SGR 颜色码」填充：glamour 会把每行补足到换行宽度，
// 这些尾部填充在终端里不可见，却会拉长行宽（逼近终端宽度易触发折行）并产生大量噪声 ANSI。
var trailingPad = regexp.MustCompile(`(?:\x1b\[[0-9;]*m|[ \t])+$`)

// markdownStyle 基于 glamour 暗色主题，但大幅「减负」让终端里更干净：
//   - 标题：去掉 "## …" 字面前缀与 H1 的背景徽标，只留加粗+配色（真正被解析掉）
//   - 行内代码：去掉背景方框，只用一个柔和的色
//   - 代码块：关掉语法高亮（chroma），ASCII 图/普通文本不再被染成花花绿绿
//   - 文档边距归零，贴着行首（圆点下方）对齐
func markdownStyle() ansi.StyleConfig {
	s := styles.DarkStyleConfig
	for _, h := range []*ansi.StyleBlock{&s.H1, &s.H2, &s.H3, &s.H4, &s.H5, &s.H6} {
		h.Prefix, h.Suffix = "", ""
		h.BackgroundColor = nil
	}
	s.Code.BackgroundColor = nil // 行内代码去掉背景框
	s.CodeBlock.Chroma = nil      // 代码块关语法高亮，整体单色更干净
	var zero uint
	s.Document.Margin = &zero
	return s
}

// markdown 渲染器与上一次结果都按宽度缓存：glamour 构造较重、解析也不便宜，
// 流式途中每个 token 都会重渲染，故对「同文本同宽度」直接复用上次结果（spinner 跳动不重算）。
var (
	mdMu         sync.Mutex
	mdRenderer   *glamour.TermRenderer
	mdWidth      int
	mdLastKey    string
	mdLastOut    string
	mdLastRender time.Time
)

// renderMarkdown 把 markdown 渲染成带 ANSI 的终端文本（暗色主题、代码高亮、按 width 换行）。
// 实时可用：流式途中对累积文本反复调用即可，半截语法（未闭合 ** 或代码块）随补全自动成形。
// force=false 时做时间节流（约 8/s），返回上次结果以免每 token 都跑 glamour；force=true（刷入
// 回滚区时）必出准确结果。失败时回退原文，绝不丢内容。
func renderMarkdown(text string, width int, force bool) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	if width < 20 {
		width = 20
	}

	mdMu.Lock()
	key := strconv.Itoa(width) + "\x00" + text
	if key == mdLastKey && mdLastOut != "" { // 文本未变：直接复用（spinner 跳动等）
		out := mdLastOut
		mdMu.Unlock()
		return out
	}
	if !force && mdLastOut != "" && time.Since(mdLastRender) < mdThrottle {
		out := mdLastOut // 节流窗口内：先返回上次结果（略滞后，下次再追上）
		mdMu.Unlock()
		return out
	}
	if mdRenderer == nil || mdWidth != width {
		r, err := glamour.NewTermRenderer(
			glamour.WithStyles(markdownStyle()),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			mdMu.Unlock()
			return text
		}
		mdRenderer, mdWidth = r, width
	}
	r := mdRenderer
	mdMu.Unlock()

	out, err := r.Render(text)
	if err != nil {
		return text // 渲染失败：原样返回，保证不丢内容
	}
	// 去掉每行尾部不可见的填充，再去掉首尾空行，让 markdown 块贴合行内排版
	lines := strings.Split(out, "\n")
	for i, ln := range lines {
		lines[i] = trailingPad.ReplaceAllString(ln, "")
	}
	out = strings.Trim(strings.Join(lines, "\n"), "\n")

	mdMu.Lock()
	mdLastKey, mdLastOut, mdLastRender = key, out, time.Now()
	mdMu.Unlock()
	return out
}
