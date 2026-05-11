# cc-mini-go

> 一个**零第三方依赖**、仅使用 Go 官方标准库实现的 Code Agent 核心框架，兼容 OpenAI `ChatCompletion` 协议。

## 项目特点

- **纯官方库**：`net/http`、`encoding/json`、`sync`、`bufio` 等，`go.mod` 无任何第三方依赖
- **ChatCompletion 协议兼容**：可直连 OpenAI、或任何兼容 `/chat/completions` 协议的网关
- **流式 & 非流式双模式**：支持一次性响应与 SSE 流式响应，后者通过回调函数逐 chunk 消费
- **工具调用（Function Calling）闭环**：Agent 内部自动识别 `tool_calls` → 并发执行本地函数 → 回灌 `role: "tool"` 消息 → 再次请求模型，循环直到得出最终答复


## 已完成

- [x] 实现了chat completion 协议兼容
- [x] 完成了简单的工具和Agent实现

## 快速开始

### 1. 准备配置文件

在用户目录下创建 `~/.cc_mini_go/setting.json`：

```json
{
  "base_url": "https://api.openai.com/v1",
  "api_key": "sk-xxxxxxxxxxxxxxxx",
  "model": "gpt-4o-mini"
}
```

> `base_url` 必须以 `http://` 或 `https://` 开头，路径以 `/v1` 结尾（内部会拼接 `/chat/completions`）。

### 2. 最小使用示例

```go
package main

import (
	"fmt"

	"github.com/yinxiangpingfan/cc-mini-go/agent"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/config"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

func main() {
	cfg, _ := config.GetConfig()
	cl, _ := client.Init(cfg.ApiUrl, cfg.ApiKey)
	cm := client.NewChatCompletionMessage()
	call := client.NewCall(cl, cm)

	a := agent.NewChatCompletionAgent(&cfg, call)
	msgs, err := a.Agent([]client.Message{
		{Role: "user", Content: "东京现在几点？"},
	}, prompt.SystemPrompt)

	if err != nil {
		panic(err)
	}
	for _, m := range msgs {
		fmt.Printf("%+v\n", m)
	}
}
```

### 3. 运行测试

```bash
go test ./test/... -v
```

## 🧩 核心模块

### Client（[client/](client/)）

- `Init(baseUrl, apiKey)` 构造 HTTP 客户端
- `NewCallRequest(model, messages, stream, system, tools, streamFunc)` 发起请求
  - `messages []any` 统一承载 `Message` / `ToolsMessage` 等异构消息，JSON 序列化时符合 OpenAI 协议
  - `stream=true` 时走 `bufio.Scanner` 解析 SSE，通过回调 `func(StreamResponse)` 逐 chunk 交付

### Message 结构（[client/message.go](client/message.go)）

与 OpenAI ChatCompletion 一一对应：

- `Message{Role, Content, ToolCalls}` — system / user / assistant 通用结构
- `ToolsMessage{Role:"tool", Content, ToolsId}` — 工具执行结果回灌
- `Tool / FunctionDefinition / FunctionParameters` — 工具声明（JSON Schema）
- `CallResponse / Choice / ResponseMessage / ToolCall` — 非流式响应
- `StreamResponse / StreamChoice / StreamDelta / StreamToolCall` — 流式响应（工具调用支持增量拼装）

### Agent（[agent/agent.go](agent/agent.go)）

Agent 主循环逻辑：

```
┌───────────────────────────────────────────────┐
│ 1. 发起 ChatCompletion 请求                    │
│ 2. 若 Refusal 非空 → 返回拒绝信息              │
│ 3. 若 Content 非空 → 追加 assistant 消息        │
│ 4. 若 ToolCalls 非空:                          │
│    - 追加 assistant tool_calls 消息             │
│    - 并发执行所有工具函数（WaitGroup + Mutex）  │
│    - 每个结果以 role:"tool" 追加到消息数组      │
│    - 回到步骤 1                                 │
│ 5. 否则返回完整消息链                           │
└───────────────────────────────────────────────┘
```

### Tools（[tools/](tools/)）

工具注册约定：

```go
type Tools struct {
    Name string
    Func func(input map[string]any) string   // 返回 JSON 字符串
}
```

每个工具需额外暴露一个 `XxxInfoForLLm() client.Tool`，用于向模型声明 JSON Schema。

**新增一个工具**三步走：

1. 在 `tools/` 下新建文件，实现 `func NewXxxTool() *Tools`
2. 实现 `func (t *Tools) XxxInfoForLLm() client.Tool` 返回 schema
3. 在 Agent 中注册到 `tools` map 并加入 `NewCallRequest` 的 `tools` 参数

## 内置工具

| 工具名      | 功能                           | 参数                           |
| ----------- | ------------------------------ | ------------------------------ |
| `time_now`  | 获取指定 IANA 时区的当前时间    | `region` (string, 必填)        |


## License

MIT
