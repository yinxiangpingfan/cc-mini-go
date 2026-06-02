// Command tui 是 cc-mini-go 的终端交互界面（基于 Bubble Tea）。
//
// 它是核心包的「消费者」：core 的 agent/client/agent_tools 仍只依赖标准库，
// 只有本模块（独立 go.mod + replace）引入 UI 第三方库。
//
// 用法：在本目录执行 `go run .`（需先配置好 ~/.cc_mini_go/setting.json）。
package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yinxiangpingfan/cc-mini-go/agent"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/config"
)

// eventBufferSize 事件 channel 缓冲。要足够大，否则高频 token 会被非阻塞 emit 丢弃（见 docs/tui-integration.md §3）。
const eventBufferSize = 512

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tui:", err)
		os.Exit(1)
	}
}

func run() error {
	// 1. 把日志重定向到文件，避免 slog 写 stdout 污染 TUI 画面。
	closeLog, err := redirectLogToFile()
	if err != nil {
		return err
	}
	defer closeLog()

	// 2. 校验并加载配置（缺失时给出友好提示）。
	cf, err := config.GetConfig()
	if err != nil {
		return fmt.Errorf("读取配置失败（请检查 ~/.cc_mini_go/setting.json）: %w", err)
	}

	// 3. 装配 client 与 agent，注入事件 channel。
	cl, err := client.Init(cf.ApiUrl, cf.ApiKey)
	if err != nil {
		return fmt.Errorf("初始化 client 失败: %w", err)
	}
	cm := client.NewChatCompletionMessage()
	call := client.NewCall(cl, cm, slog.Default())
	events := make(chan agent.AgentEvent, eventBufferSize)
	ag := agent.NewChatCompletionAgent(&cf, call, agent.WithEventChannel(events))

	// 4. 启动 Bubble Tea。用指针 model 以便后台 goroutine 拿到 program 句柄。
	m := newModel(ag, cm, events, cf.Model)
	p := tea.NewProgram(m, tea.WithAltScreen())
	m.program = p

	_, err = p.Run()
	return err
}

// redirectLogToFile 把全局 slog 输出导向 ~/.cc_mini_go/tui.log。
// 返回的关闭函数在退出时调用。失败不致命，退而求其次丢弃日志。
func redirectLogToFile() (func(), error) {
	home, err := os.UserHomeDir()
	if err != nil {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.NewFile(0, os.DevNull), nil)))
		return func() {}, nil
	}
	dir := filepath.Join(home, ".cc_mini_go")
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return func() {}, nil
	}
	f, err := os.OpenFile(filepath.Join(dir, "tui.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return func() {}, nil
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo})))
	return func() { _ = f.Close() }, nil
}
