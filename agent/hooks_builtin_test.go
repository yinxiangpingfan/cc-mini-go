package agent

import (
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/agent/core"
	"github.com/yinxiangpingfan/cc-mini-go/config"
)

// SessionStart 由 sessionOnce 守护：多次调用只触发一次。
func TestFireSessionStart_FiresOnce(t *testing.T) {
	r := core.NewHookRunner()
	n := 0
	r.Register(core.HookSessionStart, func(map[string]any) core.HookResult {
		n++
		return core.HookResult{}
	})
	a := &ChatCompletionAgent{hooks: r, cf: &config.Config{}}

	a.fireSessionStart()
	a.fireSessionStart()
	a.fireSessionStart()
	if n != 1 {
		t.Fatalf("SessionStart should fire exactly once, fired %d", n)
	}
}

// WithBuiltinHooks 注册内置 hook：SessionStart 触发后向 UI 发出一条 EventHookNotice。
func TestWithBuiltinHooks_EmitsSessionNotice(t *testing.T) {
	ch := make(chan AgentEvent, 8)
	a := NewChatCompletionAgent(&config.Config{}, nil,
		WithEventChannel(ch), WithBuiltinHooks())

	a.fireSessionStart()

	select {
	case ev := <-ch:
		if ev.Type != EventHookNotice {
			t.Fatalf("want EventHookNotice, got %s", ev.Type)
		}
	default:
		t.Fatal("builtin SessionStart hook should emit an event")
	}
}

// 内置审计 hook（PostToolUse）只观察、不拦截。
func TestBuiltinAuditHook_DoesNotBlock(t *testing.T) {
	a := NewChatCompletionAgent(&config.Config{}, nil, WithBuiltinHooks())
	got := a.runHook(core.HookPostToolUse, map[string]any{"tool_name": "bash", "output": "ok"})
	if got.ExitCode != core.HookContinue {
		t.Fatalf("audit hook should observe only, got exit=%d", got.ExitCode)
	}
}

// 内置 hook 可叠加到自定义 runner 上：WithHooks + WithBuiltinHooks 同时生效。
func TestWithBuiltinHooks_StacksOnCustomRunner(t *testing.T) {
	custom := core.NewHookRunner()
	customRan := false
	custom.Register(core.HookSessionStart, func(map[string]any) core.HookResult {
		customRan = true
		return core.HookResult{}
	})
	ch := make(chan AgentEvent, 8)
	a := NewChatCompletionAgent(&config.Config{}, nil,
		WithEventChannel(ch), WithHooks(custom), WithBuiltinHooks())

	a.fireSessionStart()

	if !customRan {
		t.Fatal("custom SessionStart handler should still run")
	}
	select {
	case ev := <-ch:
		if ev.Type != EventHookNotice {
			t.Fatalf("want builtin EventHookNotice too, got %s", ev.Type)
		}
	default:
		t.Fatal("builtin handler should also have emitted")
	}
}
