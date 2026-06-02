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

	"github.com/yinxiangpingfan/cc-mini-go/agent"
)

// resize 根据终端尺寸重排：transcript 占主区，输入框 + 帮助行固定在底部。
func (m *model) resize(w, h int) {
	m.width, m.height = w, h

	// 垂直预算：输入块(inputHeight + 边框2) + 帮助行(1)
	vpHeight := h - (inputHeight + 2) - 1
	if vpHeight < 1 {
		vpHeight = 1
	}
	if !m.ready {
		m.viewport = viewport.New(w, vpHeight)
		m.ready = true
	} else {
		m.viewport.Width = w
		m.viewport.Height = vpHeight
	}
	m.input.SetWidth(w - 2)
	m.refreshViewport()
}

// refreshViewport 重建 transcript 内容并在运行中自动滚到底部。
func (m *model) refreshViewport() {
	if !m.ready {
		return
	}
	atBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.renderTranscript(m.viewport.Width))
	if m.running || atBottom {
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
			if m.showThink {
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
			b.WriteString(mark + " " + toolStyle.Render(toolDescriptor(e.toolName, e.toolArgs)))
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
		}
	}

	// 底部工作行：重试提示优先，否则显示拟人化计时 spinner
	if m.running {
		if m.retry != "" {
			b.WriteString(m.spinner.View() + " " + retryStyle.Render(m.retry))
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
	help := helpStyle.Render(m.helpLine())
	return strings.Join([]string{
		m.viewport.View(),
		inputBorderStyle.Render(m.input.View()),
		help,
	}, "\n")
}

func (m *model) helpLine() string {
	if m.running {
		return "ctrl+c 中断 · ctrl+t 思考 · pgup/pgdn 滚动"
	}
	return "enter 发送 · ctrl+j 换行 · ctrl+t 思考 · pgup/pgdn 滚动 · ctrl+c 退出"
}

// ---- 渲染辅助 ----

// toolDescriptor 把工具名 + 原始参数 JSON 压成一行可读描述，如 bash(go build ./...)。
func toolDescriptor(name, argsJSON string) string {
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
	return fmt.Sprintf("%s(%s)", name, truncate(key, 72))
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

// retryLine 组装重试提示文案。
func retryLine(ev agent.AgentEvent) string {
	return fmt.Sprintf("重试 %d/%d，%.1fs 后（%v）", ev.Attempt, ev.Max, ev.Delay.Seconds(), ev.Err)
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
