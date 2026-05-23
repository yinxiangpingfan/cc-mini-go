# cc-mini-go

[English](README_EN.md)

> 一个**零第三方依赖**、仅使用 Go 官方标准库实现的 Code Agent 核心框架，兼容 OpenAI ChatCompletion 协议。

## 特性

- **纯标准库** - `net/http`、`encoding/json`、`sync`、`bufio` 等，`go.mod` 无任何第三方依赖
- **ChatCompletion 协议兼容** - 可直连 OpenAI、DeepSeek、七牛云或任何兼容 `/chat/completions` 的 API
- **流式 & 非流式双模式** - SSE 流式通过回调逐 chunk 消费，非流式一次性 JSON 响应
- **工具调用闭环** - 自动识别 `tool_calls` -> 并发执行本地函数 -> 回灌 `role:"tool"` 消息 -> 循环直到最终回答
- **推理模型支持** - 处理 `reasoning_content` 字段，兼容思考类模型（DeepSeek-R1 等）
- **二进制文件检测** - 扩展名白名单 + null 字节检测 + MIME 嗅探

## 项目结构

```
cc-mini-go/
├── agent/             # Agent 主循环（流式 & 非流式）
│   ├── agent.go       # 非流式 Agent，工具调用闭环
│   └── agent_stream.go # SSE 流式 Agent，支持推理模型
├── agent_tools/       # 内置工具实现
│   ├── global.go      # 工具接口定义
│   ├── time.go        # time_now（IANA 时区）
│   ├── read.go        # read_file（二进制检测、分页）
│   ├── edit.go        # edit_file（开发中）
│   └── write.go       # write_file（开发中）
├── client/            # HTTP 客户端 & 协议类型
│   ├── init.go        # 客户端初始化
│   ├── call.go        # 请求分发（流式 & 非流式）
│   └── message.go     # 全部消息类型定义
├── config/            # 配置加载（~/.cc_mini_go/setting.json）
├── errors/            # 哨兵错误定义
├── log/               # 基于 slog 的日志
├── prompt/            # 系统提示词 & 工具提示词模板
├── tools/             # 共享工具函数（二进制文件检测）
└── test/              # 集成测试 & 单元测试
```

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

### 2. 使用示例

```go
package main

import (
    "fmt"
    "github.com/yinxiangpingfan/cc-mini-go/agent"
    "github.com/yinxiangpingfan/cc-mini-go/client"
    "github.com/yinxiangpingfan/cc-mini-go/config"
    "github.com/yinxiangpingfan/cc-mini-go/log"
    "github.com/yinxiangpingfan/cc-mini-go/prompt"
)

func main() {
    cfg, _ := config.GetConfig()
    cl, _ := client.Init(cfg.ApiUrl, cfg.ApiKey)
    logger := log.InitLogger()
    cm := client.NewChatCompletionMessage()
    call := client.NewCall(cl, cm, logger)
    a := agent.NewChatCompletionAgent(&cfg, call)

    // 非流式
    msgs, _ := a.Agent([]client.Message{
        *cm.NewUserMessage("东京现在几点？"),
    }, prompt.SystemPrompt)

    // 流式（实时打印思考过程 + 最终回答）
    msgs, _ = a.StreamAgent([]client.Message{
        *cm.NewUserMessage("读取 /tmp/example.go 并解释代码"),
    }, prompt.SystemPrompt)

    for _, m := range msgs {
        fmt.Printf("%+v\n", m)
    }
}
```

### 3. 运行测试

```bash
# 运行全部测试
go test -v ./...

# 运行指定测试（使用正则锚点精确匹配）
go test -v ./test -run '^TestAgent$'

# read_file 单元测试
go test -v ./agent_tools/ -run TestReadFile
```

## Agent 循环流程

```
用户消息
     |
     v
+----------+     有 tool_calls?    +-----------+
|  LLM API |  ───── 是 ─────────> | 执行工具   |
|   请求    |                      | （并发）   |
+----------+                      +-----------+
     ^                                 |
     |                                 |
     +──── 追加工具结果到消息列表 ──────+
     |
     无 tool_calls
     |
     v
  返回最终回复
```

## 内置工具

| 工具 | 功能 | 参数 |
|------|------|------|
| `time_now` | 获取指定时区的当前时间 | `region`（string，必填，IANA 格式如 `Asia/Tokyo`） |
| `read_file` | 读取文件内容，支持行号、二进制检测、分页 | `file_path`（必填）、`offset`（默认 1）、`limit`（默认 2000） |

## 添加新工具

1. 在 `agent_tools/` 下新建文件，实现 `func NewXxxTool() *Tools`
2. 实现 `func (t *Tools) XxxInfoForLLm() client.Tool` 返回 JSON Schema
3. 在 Agent 中注册到 `tools` map，并加入 `NewCallRequest` 的 tools 参数

```go
// 工具接口
type Tools struct {
    Name string
    Func func(input map[string]any) string  // 返回 JSON 字符串
}
```

## 架构决策

- **`[]any` 消息历史** - 异构消息类型（`Message`、`ToolsMessage`、`ResponseMessage`）共存于同一切片，通过 Go 的 JSON 接口分发正确序列化
- **`content: null` vs `""`** - 当 assistant 只有工具调用没有文本时，`Content` 设为 `nil`（序列化为 `null`），符合 API 协议预期
- **流式 tool_call ID 处理** - 部分 API 在后续 SSE chunk 中发送 `"id": ""`，解析器仅接受非空 ID，防止覆盖首个 chunk 中的正确值

## License

MIT
