# cc-mini-go

> A **zero-dependency** Code Agent framework built entirely with Go standard library, compatible with OpenAI ChatCompletion protocol.

## Features

- **Pure Standard Library** - `net/http`, `encoding/json`, `sync`, `bufio`, etc. No third-party dependencies in `go.mod`
- **ChatCompletion Compatible** - Works with OpenAI, DeepSeek, Qiniu, or any `/chat/completions` API
- **Streaming & Non-streaming** - SSE streaming with chunk-by-chunk callback, or one-shot JSON response
- **Tool Call Loop** - Auto-detects `tool_calls` -> concurrent local execution -> feeds back `role:"tool"` -> loops until final answer
- **Reasoning Model Support** - Handles `reasoning_content` for thinking models (DeepSeek-R1, etc.)
- **Binary File Detection** - Extension whitelist + null-byte detection + MIME sniffing
- **Write Safety Protection** - SHA256 hash-based read-before-write mechanism, prevents blind overwrites and detects external modifications

## Project Structure

```
cc-mini-go/
├── agent/             # Agent loop (streaming & non-streaming)
│   ├── agent.go       # Non-streaming agent with tool call loop
│   └── agent_stream.go # SSE streaming agent with reasoning support
├── agent_tools/       # Built-in tool implementations
│   ├── global.go      # Tool interface definition & ReadFiles state
│   ├── time.go        # time_now (IANA timezone)
│   ├── read.go        # read_file (with binary detection, pagination, hash tracking)
│   ├── edit.go        # edit_file (WIP)
│   └── write.go       # write_file (hash-verified write protection)
├── client/            # HTTP client & protocol types
│   ├── init.go        # Client initialization
│   ├── call.go        # Request dispatch (stream & non-stream)
│   └── message.go     # All message type definitions
├── config/            # Configuration loading (~/.cc_mini_go/setting.json)
├── errors/            # Sentinel error definitions
├── log/               # slog-based logger
├── prompt/            # System & tool prompt templates
├── tools/             # Shared utilities (binary detection, SHA256 hash)
└── test/              # Integration & unit tests
```

## Quick Start

### 1. Configuration

Create `~/.cc_mini_go/setting.json`:

```json
{
  "base_url": "https://api.openai.com/v1",
  "api_key": "sk-xxxxxxxxxxxxxxxx",
  "model": "gpt-4o-mini"
}
```

> `base_url` must start with `http://` or `https://`, ending with `/v1` (the client appends `/chat/completions`).

### 2. Usage Example

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

    // Non-streaming
    msgs, _ := a.Agent([]client.Message{
        *cm.NewUserMessage("What time is it in Tokyo?"),
    }, prompt.SystemPrompt)

    // Streaming (prints reasoning + content in real-time)
    msgs, _ = a.StreamAgent([]client.Message{
        *cm.NewUserMessage("Read /tmp/example.go and explain it"),
    }, prompt.SystemPrompt)

    for _, m := range msgs {
        fmt.Printf("%+v\n", m)
    }
}
```

### 3. Running Tests

```bash
# All tests
go test -v ./...

# Specific test (use regex anchor for exact match)
go test -v ./test -run '^TestAgent$'

# read_file unit tests
go test -v ./agent_tools/ -run TestReadFile
```

## Agent Loop

```
User Message
     |
     v
+----------+     tool_calls?     +-----------+
|  LLM API |  ───── yes ──────> | Execute   |
|  Request  |                    | Tools     |
+----------+                    | (parallel)|
     ^                          +-----------+
     |                               |
     +──── append tool results ──────+
     |
     no tool_calls
     |
     v
  Return Final Response
```

## Built-in Tools

| Tool | Description | Parameters |
|------|-------------|------------|
| `time_now` | Get current time in specified timezone | `region` (string, required, IANA format) |
| `read_file` | Read file with line numbers, binary detection, pagination | `file_path` (required), `offset` (default 1), `limit` (default 2000) |
| `write_file` | Write file with read-before-write hash verification | `file_path` (required), `content` (required) |

## Adding a New Tool

1. Create a file in `agent_tools/`, implement `func NewXxxTool() *Tools`
2. Implement `func (t *Tools) XxxInfoForLLm() client.Tool` returning JSON Schema
3. Register in agent's `tools` map and add to `NewCallRequest` tools parameter

```go
// Tool interface
type Tools struct {
    Name string
    Func func(input map[string]any) string  // Returns JSON string
}
```

## Architecture Decisions

- **`[]any` for message history** - Heterogeneous message types (`Message`, `ToolsMessage`, `ResponseMessage`) coexist in a single slice, serialized correctly via Go's JSON interface dispatch
- **`content: null` vs `""`** - When assistant has no text content (only tool_calls), `Content` is set to `nil` (serializes as `null`) to comply with API expectations
- **Stream tool_call ID handling** - Some APIs send `"id": ""` in subsequent SSE chunks; the parser only accepts non-empty IDs to prevent overwriting
- **SHA256 write protection** - `read_file` records file hash; `write_file` verifies hash consistency before overwriting. If the file was externally modified, write is rejected until re-read

## License

MIT
