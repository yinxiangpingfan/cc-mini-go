# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`cc-mini-go` is a **zero-third-party-dependency** Code Agent framework built entirely on the Go standard library, compatible with the OpenAI ChatCompletion protocol (`/chat/completions`). `go.mod` must stay dependency-free — use only stdlib (`net/http`, `encoding/json`, `sync`, `bufio`, `log/slog`, etc.). Do not add external modules.

This is a teaching codebase that incrementally implements a coding agent across chapters s00–s19 (tool calling → planning → subagents → skills → context compaction → …). Each chapter typically adds one tool plus its tests.

## Commands

```bash
go build ./...                        # build everything (run after any change)
go vet ./...                          # static analysis
go test -race ./agent_tools/          # unit tests (pure, no network) — the main suite
go test -race ./agent_tools/ -run TestSubAgentRun_ReturnsSummary -v   # single test
go test ./test/                       # integration tests — HIT THE REAL LLM API, see warning below
```

**`./test/` makes live API calls** (`TestAgent`, `TestCall`, `TestToolStream`, etc.) and depends on `~/.cc_mini_go/setting.json`. They are slow, rate-limitable (429), and will fail without valid credentials. **Never run `go test ./...` from inside an agent loop** — the integration tests spawn their own API calls and will exhaust the rate limit. For verification during development, run `go test -race ./agent_tools/` only.

## Configuration

Runtime config is read from `~/.cc_mini_go/setting.json` (NOT in the repo, NOT env vars):

```json
{ "base_url": "https://.../v1", "api_key": "sk-...", "model": "..." }
```

`base_url` must start with `http://` or `https://`. `config.GetConfig()` loads it; `client.Init` validates the URL.

## Architecture

### The agent loop (the heart of the system)

Two near-identical loops, `agent/agent.go` (`Agent`, non-streaming) and `agent/agent_stream.go` (`StreamAgent`, SSE). Both do:

1. Convert `[]client.Message` → `allMsg []any` (heterogeneous message slice).
2. `ToolInit(&tools)` registers every tool into a `map[string]func(map[string]any) string` and returns the parallel `[]client.Tool` schema list.
3. **Augment the system prompt** with the skill catalog (`withSkillCatalog`) and the plan-reminder counter.
4. Loop: call LLM → if `tool_calls` present, append the assistant message, run all tool funcs **concurrently** (`sync.WaitGroup` + `sync.Mutex` guarding `allMsg`), append each `role:"tool"` result, repeat. → if no tool calls, return.

Tool functions run in goroutines **without recover**, so a panic in any tool crashes the whole process — tool code must never panic (always use comma-ok type assertions on `args`).

### `[]any` message history

Heterogeneous message structs (`client.Message`, `client.ToolsMessage`, `client.ResponseMessage`) coexist in one `[]any` slice and serialize correctly via Go's JSON marshaling. Key protocol detail: when an assistant turn has only tool calls and no text, `Content` is `nil` (serializes to `null`), which the API requires.

### Tool authoring pattern

Every tool in `agent_tools/` follows the same shape (see `read.go`/`write.go` as the reference implementations):

```go
type Tools struct {                                    // defined in global.go
    Name string
    Func func(input map[string]any) string             // returns a JSON string
}

func NewXxxTool() *Tools { ... }                        // constructor; Func validates args, does work
func (t *Tools) XxxInfoForLLm() client.Tool { ... }     // returns the OpenAI JSON-schema for the tool
```

Then register both in `agent/tool_init.go` (`ToolInit`): add to the `tools` map AND the returned `[]client.Tool`.

- Tool results — success and error alike — are **JSON strings**. Errors use `jsonErr(msg)` → `{"error": "..."}` (in `global.go`).
- `FunctionParameters.Properties` is `map[string]any` to support nested schemas (e.g. `todo_list`'s array-of-objects). Simple params use `client.ParameterProperty{}`; nested ones use raw `map[string]any{}`.

### Error handling convention

All tool errors funnel through sentinel values in the `errors/` package, then `jsonErr(err.Error())`. Param-missing always uses `fmt.Sprintf(errors.ErrToolFunctionCall, "<argname>")`. Wrap underlying errors with `fmt.Errorf("%w: %w", errors.ErrXxx, err)` so the real cause reaches the LLM. Do not hardcode error strings in tool files — add a constant to `errors/agent_tools.go`.

### Shared concurrent state (`agent_tools/global.go`)

Package-level state guarded by `sync.RWMutex`, mutated across tool calls within a session:
- `ReadFiles` — SHA256 hash of every file read. `write_file` enforces **read-before-write**: it refuses to overwrite a file not previously read, or one whose hash changed since the read (external-modification guard).
- `ToDoList` (`PlanningState`) — the current plan plus `RoundsSinceUpdate`. The agent loop increments the counter each round (only when a plan exists) and injects a `<reminder>` once it reaches the threshold.

### Skill system (on-demand knowledge)

`agent_tools/skill.go`: a two-layer model. The lightweight **catalog** (name + description only) is injected into the system prompt; the full skill **body** is loaded on demand via the `load_skill` tool. `SkillRegistry` scans `SKILL.md` files (YAML frontmatter + body, supports block scalars) from `~/.cc_mini_go/skills` (global) then `<cwd>/.cc_mini_go/skills` (project, overrides global). The package-level `Skills` registry is built at init from `DefaultSkillDirs()`.

### Subagents (context isolation)

`agent_tools/subagent.go`: the `task` tool spawns a child loop (`SubAgentRunner.run`) with a **fresh message list** — no shared history with the parent. The child gets a filtered tool set (`buildChildTools`) that deliberately **excludes `task` itself** to prevent infinite recursion, runs up to `subAgentMaxTurns` (30), and returns only a final text summary; the child context is discarded.

## Conventions

- `--style go_zero`-adjacent naming is not used here; match the existing file's style. Comments in this repo are predominantly Chinese — follow the surrounding file.
- Note the existing typo `ReadFileInfoForLLm` / `TimeNowInfoForLLm` (lowercase `m`) vs `SubAgentInfoForLLM` / `LoadSkillInfoForLLM` (uppercase). Match whatever the file already uses; don't mass-rename.
- `.cc_mini_go/` and `temp.md` are gitignored — local skill packs and scratch notes live there and are not committed.
