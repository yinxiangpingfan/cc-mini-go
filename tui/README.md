# cc-mini-go TUI

基于 [Bubble Tea](https://github.com/charmbracelet/bubbletea) 的终端交互界面，是核心 agent 的消费者。

## 为什么是独立模块

核心包（`agent` / `client` / `agent_tools` …）坚持**零三方依赖**，根 `go.mod` 只用标准库。
本目录是**独立的 Go 模块**（自带 `go.mod`），用 `replace` 指回上层：

```
replace github.com/yinxiangpingfan/cc-mini-go => ../
```

这样 UI 第三方库（bubbletea / lipgloss / bubbles）只进本模块，绝不污染核心。

## 运行

先配置好 `~/.cc_mini_go/setting.json`（`base_url` / `api_key` / `model`），然后：

```bash
cd tui
go run .
```

构建二进制：

```bash
cd tui && go build -o cctui . && ./cctui
```

## 快捷键

| 键 | 作用 |
|----|------|
| `Enter` | 发送消息（运行中禁用） |
| `Ctrl+J` | 输入框内换行 |
| `Ctrl+T` | 展开/折叠思考流 |
| `PgUp` / `PgDn` | 滚动对话区 |
| `Ctrl+C` | 运行中=停止当前轮；空闲时=退出 |

## 设计要点（对应 `docs/tui-integration.md`）

- **并发模型**：`StreamAgent` 跑在后台 goroutine，进度通过 `WithEventChannel` 注入的事件 channel
  （缓冲 512）单向流出；UI 用一个 `waitEvent` 监听循环把事件桥成 `tea.Msg`，全程只保留一个监听者，
  保证事件顺序。后台 goroutine 从不直接改 UI 状态，结束时用 `program.Send(doneMsg{...})` 通知。
- **取消**：每轮持有 `context.CancelFunc`，`Ctrl+C` 调用它，中断进行中的 LLM 请求与可取消工具
  （bash/subagent/grep/glob），`StreamAgent` 随即返回 `context.Canceled`。
- **日志重定向**：核心若经 slog 写 stdout 会污染画面，启动时把全局 slog 导向 `~/.cc_mini_go/tui.log`。
- **多轮历史**：TUI 侧只保留纯文本 user/assistant 轮次重新发送（见 docs §5），工具历史不回传。
- **掉帧兜底**：事件 emit 非阻塞、满则丢弃；即使实时预览掉几个 token，最终完整文本仍由 `StreamAgent` 返回值固化。
