# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`cc-mini-go` is a **zero-third-party-dependency** Code Agent framework built entirely on the Go standard library, compatible with the OpenAI ChatCompletion protocol (`/chat/completions`). The **core** `go.mod` must stay dependency-free — use only stdlib (`net/http`, `encoding/json`, `sync`, `bufio`, `log/slog`, etc.). Do not add external modules to the core.

This is a teaching codebase that incrementally implements a coding agent across chapters s00–s19 (tool calling → planning → subagents → skills → context compaction → permission → hooks → memory → system-prompt pipeline → …). Each chapter typically adds one tool or subsystem plus its tests. Implemented through **s10 (system-prompt assembly pipeline)** so far.

### Two-module layout

There are **two** Go modules:

- **Core** (repo root, `github.com/yinxiangpingfan/cc-mini-go`): the agent, client, tools, prompt, etc. **Stdlib only.**
- **TUI** (`tui/`, separate `go.mod` with a `replace` back to the core): the terminal UI built on Bubble Tea. This is the **only** place third-party dependencies (charmbracelet/*) are allowed — it keeps the core clean. Build/test it from inside `tui/`.

## Commands

```bash
go build ./...                        # build the core (run after any change)
go vet ./...                          # static analysis
go test -race ./agent_tools/          # tool unit tests (pure, no network) — the main suite
go test -race ./agent/ ./agent/core/  # agent wiring + pure-mechanism tests (permission/hook)
go test -race ./agent_tools/ -run TestSubAgentRun_ReturnsSummary -v   # single test
go test ./test/                       # integration tests — HIT THE REAL LLM API, see warning below

cd tui && go build ./... && go vet ./... && go test .   # the TUI module (separate go.mod)
```

**`./test/` makes live API calls** (`TestAgent`, `TestCall`, `TestToolStream`, etc.) and depends on `~/.cc_mini_go/setting.json`. They are slow, rate-limitable (429), and will fail without valid credentials. **Never run `go test ./...` from inside an agent loop** — the integration tests spawn their own API calls and will exhaust the rate limit. For verification during development, run `go test -race ./agent_tools/ ./agent/ ./agent/core/` only.

## Configuration

Runtime config is read from `~/.cc_mini_go/setting.json` (NOT in the repo, NOT env vars):

```json
{ "base_url": "https://.../v1", "api_key": "sk-...", "model": "...", "permission": { ... } }
```

`base_url` must start with `http://` or `https://`. `config.GetConfig()` loads it; `client.Init` validates the URL. The optional `permission` block configures the permission engine (mode + rules).

## Architecture

### Layering: `core` (mechanism) → `agent` (wiring) → `tui` (consumer)

A recurring pattern: a **pure, stdlib-only mechanism** lives in `agent/core/`, the **wiring** that injects it into the agent lives in `agent/`, and the **UI** only enables and displays it in `tui/`.

- `agent/core/permission.go` — `PermissionEngine`: pure allow/deny/ask decision logic.
- `agent/core/hook.go` — `HookRunner`: pure event→handler dispatch with exit codes.

`agent/core` must not import agent/client types — it's the bottom layer.

### The agent loop (the heart of the system)

Two near-identical loops, `agent/agent.go` (`Agent`, non-streaming) and `agent/agent_stream.go` (`StreamAgent`, SSE). `NewChatCompletionAgent(cf, call, opts...)` builds the agent via functional options: `WithEventChannel`, `WithPermissions`, `WithApproval`, `WithHooks`, `WithBuiltinHooks`. Both loops:

1. Copy the incoming `[]any` history (heterogeneous message slice); never mutate the caller's slice.
2. `ToolInit(&tools)` registers every tool into a `map[string]agent_tools.ToolFunc` and returns the parallel `[]client.Tool` schema list.
3. **Assemble the system prompt** via the `buildSystemPrompt` pipeline (see below). A plan `<reminder>` is injected separately, per-round, once the plan-reminder counter crosses its threshold.
4. Fire the `SessionStart` hook exactly once (`sync.Once`), since the loop re-enters per user message.
5. Apply a microcompact time-gate on the first turn when the session has been idle past the threshold; keep flushing the session transcript to disk.
6. Loop: call LLM (with retry, see `agent/retry.go`) → if `tool_calls` present, append the assistant message, run all tool funcs **concurrently** (`sync.WaitGroup` + `sync.Mutex` guarding `allMsg`) through `execToolWithHooks`, append each `role:"tool"` result (plus any image/injected messages), repeat. → if no tool calls, return.

Tool functions run in goroutines **without recover**, so a panic in any tool crashes the whole process — tool code must never panic (always use comma-ok type assertions on `args`).

### System-prompt assembly pipeline (`agent/system_prompt.go`, s10)

The system prompt is not one hardcoded string but a **segmented pipeline**. `buildSystemPrompt(core)` joins non-empty sections (`joinNonEmpty`) in order:

- **static block** (relatively stable, cache-friendly): `core` (passed in by the caller, usually `prompt.SystemPrompt`) + `sectionSkills` (skill catalog) + `sectionMemory` (memory bodies) + `sectionClaudeMD` (layered CLAUDE.md).
- a `=== DYNAMIC CONTEXT ===` boundary marker (`prompt.DynamicContextHeader`), then the **dynamic block** (`sectionDynamic`): date / cwd / model / permission mode — anything that changes between turns.

Notes: **tools are not inlined into the prompt** — they ride the OpenAI `tools` field (`ToolInit`'s `[]client.Tool`), so re-listing them as text would only waste tokens. `sectionClaudeMD` reads layered CLAUDE.md and **stacks** (does not override): user global `~/.cc_mini_go/CLAUDE.md` → project `<cwd>/CLAUDE.md`. Each section returns "" when its source is empty and is skipped. Per-round `<reminder>`s (e.g. the plan reminder) stay on a **separate channel** — they are not baked into this relatively-stable prompt.

### `execToolWithHooks` — the per-tool gauntlet (`agent/hook.go`)

Every tool call goes through: **PreToolUse hook → permission gate → execute → PostToolUse hook**. Hook exit codes: `HookContinue`(0) runs on, `HookBlock`(1) skips the tool and returns a blocked result, `HookInject`(2) runs the tool but collects an extra message. Blocked tools (hook block or permission deny) still return a paired `role:"tool"` result to preserve tool_call/result adjacency; exit-2 injected messages are appended **after** all tool results (collected into `injectedMsgs`, mirroring the image-URI pattern).

### `[]any` message history

Heterogeneous message structs (`client.Message`, `client.ToolsMessage`, `client.ResponseMessage`) coexist in one `[]any` slice and serialize correctly via Go's JSON marshaling. The `Agent`/`StreamAgent` entry points take and return `[]any` so callers close the loop with the full history. Key protocol detail: when an assistant turn has only tool calls and no text, `Content` is `nil` (serializes to `null`), which the API requires.

### Tool authoring pattern

Every tool in `agent_tools/` follows the same shape (see `read.go`/`write.go` as the reference implementations):

```go
// ToolFunc, defined in global.go:
type ToolFunc = func(ctx context.Context, input map[string]any) string  // returns a JSON string

type Tools struct {            // defined in global.go
    Name string
    Func ToolFunc `json:"-"`
}

func NewXxxTool() *Tools { ... }                        // constructor; Func validates args, does work
func (t *Tools) XxxInfoForLLm() client.Tool { ... }     // returns the OpenAI JSON-schema for the tool
```

The `ctx` is for cancelling long-running work (e.g. bash). Register both in `agent/tool_init.go` (`ToolInit`): add to the `tools` map AND the returned `[]client.Tool`.

Currently registered tools: `time_now`, `read_file` (also reads images as multimodal input), `write_file`, `bash`, `todo_list`, `task` (subagent), `load_skill`, `compact`, `edit_file`, `grep`, `glob`, `save_memory`, `delete_memory`.

- Tool results — success and error alike — are **JSON strings**. Errors use `jsonErr(msg)` → `{"error": "..."}` (in `global.go`).
- `FunctionParameters.Properties` is `map[string]any` to support nested schemas (e.g. `todo_list`'s array-of-objects). Simple params use `client.ParameterProperty{}` (which has `Type`/`Description`/`Enum`); nested ones use raw `map[string]any{}`.

### Error handling convention

All tool errors funnel through sentinel values in the `errors/` package, then `jsonErr(err.Error())`. Param-missing always uses `fmt.Sprintf(errors.ErrToolFunctionCall, "<argname>")`. Wrap underlying errors with `fmt.Errorf("%w: %w", errors.ErrXxx, err)` so the real cause reaches the LLM. Do not hardcode error strings in tool files — add a constant to `errors/agent_tools.go`.

### Shared concurrent state (`agent_tools/global.go`)

Package-level state guarded by `sync.RWMutex`, mutated across tool calls within a session:
- `ReadFiles` — SHA256 hash of every file read. `write_file`/`edit_file` enforce **read-before-write**: they refuse to overwrite a file not previously read, or one whose hash changed since the read (external-modification guard).
- `ToDoList` (`PlanningState`) — the current plan plus `RoundsSinceUpdate`. The agent loop increments the counter each round (only when a plan exists) and injects a `<reminder>` once it reaches the threshold.

### Skill system (on-demand knowledge)

`agent_tools/skill.go`: a two-layer model. The lightweight **catalog** (name + description only) is injected into the system prompt; the full skill **body** is loaded on demand via the `load_skill` tool. `SkillRegistry` scans `SKILL.md` files (YAML frontmatter + body, supports block scalars via `parseFrontmatter`) from `~/.cc_mini_go/skills` (global) then `<cwd>/.cc_mini_go/skills` (project, overrides global). The package-level `Skills` registry is built at init from `DefaultSkillDirs()`.

### Memory system (cross-session, s09)

`agent_tools/memory.go`: the **mirror image** of the skill system, but read/write and always-injected. `MemoryStore` scans flat `<name>.md` files (reusing `parseFrontmatter`, skipping the `MEMORY.md` index) from the same global→project dir pair. Unlike skills, memory is small and is "long-term direction," so `Describe()` injects the **full bodies** (grouped by type) into the system prompt at session start via `withMemory` — there is no on-demand load tool. Writes go through the `save_memory` / `delete_memory` tools (`Save`/`Delete`), which write to the project dir, rebuild `MEMORY.md`, and update the in-memory table under a `sync.RWMutex` (a real need here — tools run concurrently). Four allowed types (`MemoryTypes`): `user`, `feedback`, `project`, `reference`; the "what not to store" boundary lives in `prompt.SaveMemoryPrompt`, enforced by the model, not code. `save_memory` writes are surfaced in the TUI as a `🧠 已记住` notice.

### Subagents (context isolation)

`agent_tools/subagent.go`: the `task` tool spawns a child loop (`SubAgentRunner.run`) with a **fresh message list** — no shared history with the parent. The child gets a filtered tool set (`buildChildTools`) that deliberately **excludes `task` itself** to prevent infinite recursion, runs up to `subAgentMaxTurns` (30), and returns only a final text summary; the child context is discarded.

### Permission system (s-permission)

`agent/core/permission.go` is the pure engine (modes + rules → allow/deny/ask). `agent/permission.go` wires it into `execToolWithHooks` (the `gateToolCall` step) and bridges `ask` decisions to an `ApprovalFunc`. The TUI (`tui/permission.go`) implements that approver as a y/n confirmation box. Config comes from the `permission` block in `setting.json`.

### Hook system (s08)

Three layers, mirroring permission: `agent/core/hook.go` (pure `HookRunner`: `Register`/`Run`, exit codes), `agent/hook.go` (wiring: `WithHooks`, `runHook`, `execToolWithHooks`), `agent/hooks_builtin.go` (the **single file** where built-in handlers are registered via `registerBuiltinHooks`; enable with `WithBuiltinHooks`). Events: `SessionStart`, `PreToolUse`, `PostToolUse`. Handlers are agent methods (closures over `a`) so they can `a.emit(...)` events or `slog`. `agent/core` uses no locks for hooks (handlers register at startup, run-time is read-only). See `example/hooks/` for an offline demo.

### Context compaction

`agent_tools/compact.go`: the `compact` tool does a manual full conversation summary. The loops also run an automatic **microcompact** time-gate (compress old tool results only after the session has been idle past a gap threshold, on the first turn) and keep a `SessionTranscript` that streams the whole conversation to JSONL on disk, decoupled from compaction.

### TUI (`tui/`)

Bubble Tea terminal UI; the agent's `AgentEvent` channel drives a live transcript. Inline-render model (no alt-screen, no mouse capture): finished turns are flushed to the terminal's native scrollback via `tea.Println`, so native wheel-scroll and drag-select/copy both work; `View` renders only the in-progress region + plan panel + input box. It parses inline `<think>…</think>` into a thinking block, shows a persistent todo/plan panel from `todo_list` results, and renders hook/memory notices.

## Conventions

- `--style go_zero`-adjacent naming is not used here; match the existing file's style. Comments in this repo are predominantly Chinese — follow the surrounding file.
- Keep the **core** dependency-free; third-party deps belong only in the `tui/` module.
- Note the inconsistent casing: `ReadFileInfoForLLm` / `TimeNowInfoForLLm` (lowercase `m`) vs `SubAgentInfoForLLM` / `LoadSkillInfoForLLM` / `SaveMemoryInfoForLLM` (uppercase). Match whatever the file already uses; don't mass-rename.
- `.cc_mini_go/` and `temp.md` are gitignored — local skill packs, memory files, and scratch notes live there and are not committed.
- Commit messages: conventional-commit style, predominantly Chinese subject lines, ending with the `Co-Authored-By: Claude Opus 4.8` trailer (match recent history).
