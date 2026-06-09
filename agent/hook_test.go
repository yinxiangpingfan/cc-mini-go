package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/agent/core"
	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
)

// fakeTool 返回固定输出，并通过 ran 记录是否真的被执行（验证拦截不执行）。
func fakeTool(out string, ran *bool) shared.ToolFunc {
	return func(context.Context, map[string]any) string {
		if ran != nil {
			*ran = true
		}
		return out
	}
}

// 未配置 hook / 权限时：工具照常执行，无注入。
func TestExecToolWithHooks_NoHooksRunsTool(t *testing.T) {
	a := &ChatCompletionAgent{}
	ran := false
	content, _, _, inject := a.execToolWithHooks(context.Background(), "id", "read_file", "{}", nil, fakeTool(`{"ok":true}`, &ran))
	if !ran {
		t.Fatal("tool should run when no hooks/perms configured")
	}
	if content != `{"ok":true}` {
		t.Fatalf("content = %q, want tool output", content)
	}
	if len(inject) != 0 {
		t.Fatalf("no injection expected, got %v", inject)
	}
}

// PreToolUse 返回 block(1)：工具不执行，返回配对的「被拦截」结果。
func TestExecToolWithHooks_PreBlockSkipsTool(t *testing.T) {
	r := core.NewHookRunner()
	r.Register(core.HookPreToolUse, func(map[string]any) core.HookResult {
		return core.HookResult{ExitCode: core.HookBlock, Message: "nope"}
	})
	a := &ChatCompletionAgent{hooks: r}

	ran := false
	content, _, _, inject := a.execToolWithHooks(context.Background(), "id", "bash", "{}", nil, fakeTool("x", &ran))
	if ran {
		t.Fatal("blocked tool must not run")
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		t.Fatalf("blocked result is not valid JSON: %v (%q)", err, content)
	}
	if !strings.Contains(m["error"], "blocked by hook") {
		t.Fatalf("content %q should mark a hook block", content)
	}
	if len(inject) != 0 {
		t.Fatalf("block should carry no injection, got %v", inject)
	}
}

// PreToolUse 返回 inject(2)：工具仍执行，注入消息被收集。
func TestExecToolWithHooks_PreInjectRunsToolAndCollects(t *testing.T) {
	r := core.NewHookRunner()
	r.Register(core.HookPreToolUse, func(map[string]any) core.HookResult {
		return core.HookResult{ExitCode: core.HookInject, Message: "heads up"}
	})
	a := &ChatCompletionAgent{hooks: r}

	ran := false
	content, _, _, inject := a.execToolWithHooks(context.Background(), "id", "bash", "{}", nil, fakeTool("ok", &ran))
	if !ran {
		t.Fatal("inject(2) should still run the tool")
	}
	if content != "ok" {
		t.Fatalf("content = %q, want tool output", content)
	}
	if len(inject) != 1 || inject[0] != "heads up" {
		t.Fatalf("pre-inject not collected: %v", inject)
	}
}

// PostToolUse 能看到工具输出，并可注入补充说明。
func TestExecToolWithHooks_PostInjectSeesOutput(t *testing.T) {
	r := core.NewHookRunner()
	var gotOutput any
	r.Register(core.HookPostToolUse, func(p map[string]any) core.HookResult {
		gotOutput = p["output"]
		return core.HookResult{ExitCode: core.HookInject, Message: "logged"}
	})
	a := &ChatCompletionAgent{hooks: r}

	_, _, _, inject := a.execToolWithHooks(context.Background(), "id", "bash", "{}", nil, fakeTool("RESULT", nil))
	if gotOutput != "RESULT" {
		t.Fatalf("post hook should see tool output, got %v", gotOutput)
	}
	if len(inject) != 1 || inject[0] != "logged" {
		t.Fatalf("post-inject not collected: %v", inject)
	}
}

// 权限闸拦截时：工具不执行，但 PreToolUse 已收集的注入消息仍要带出去。
func TestExecToolWithHooks_GateBlockKeepsInjected(t *testing.T) {
	hr := core.NewHookRunner()
	hr.Register(core.HookPreToolUse, func(map[string]any) core.HookResult {
		return core.HookResult{ExitCode: core.HookInject, Message: "note"}
	})
	eng := core.NewPermissionEngine(core.ModeDefault,
		[]core.PermissionRule{{Tool: "bash", Behavior: core.PermDeny}}, nil)
	a := &ChatCompletionAgent{hooks: hr, perms: eng}

	ran := false
	content, _, _, inject := a.execToolWithHooks(context.Background(), "id", "bash", "{}",
		map[string]any{"command": "ls"}, fakeTool("x", &ran))
	if ran {
		t.Fatal("gate-denied tool must not run")
	}
	if !strings.Contains(content, "permission denied") {
		t.Fatalf("content %q should be a permission denial", content)
	}
	if len(inject) != 1 || inject[0] != "note" {
		t.Fatalf("pre-inject should survive a gate block: %v", inject)
	}
}
