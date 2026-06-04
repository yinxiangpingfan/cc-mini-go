package core

import "testing"

// 未注册任何 handler 时，Run 等价于「继续」。
func TestHookRunner_RunNoHandlers(t *testing.T) {
	r := NewHookRunner()
	if got := r.Run(HookPreToolUse, nil); got.ExitCode != HookContinue {
		t.Fatalf("no handlers should continue, got exit=%d", got.ExitCode)
	}
}

// 短路语义：谁先返回 1/2 谁优先，其后的 handler 不再执行。
func TestHookRunner_FirstBlockOrInjectWins(t *testing.T) {
	tests := []struct {
		name     string
		results  []HookResult
		wantCode int
		wantMsg  string
		wantRun  int // 期望真正被调用的 handler 数（短路后不再调用）
	}{
		{"all continue", []HookResult{{HookContinue, ""}, {HookContinue, ""}}, HookContinue, "", 2},
		{"block short-circuits", []HookResult{{HookBlock, "nope"}, {HookContinue, ""}}, HookBlock, "nope", 1},
		{"inject short-circuits", []HookResult{{HookContinue, ""}, {HookInject, "note"}, {HookBlock, "x"}}, HookInject, "note", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewHookRunner()
			run := 0
			for _, res := range tt.results {
				res := res
				r.Register(HookPreToolUse, func(map[string]any) HookResult {
					run++
					return res
				})
			}
			got := r.Run(HookPreToolUse, nil)
			if got.ExitCode != tt.wantCode || got.Message != tt.wantMsg {
				t.Fatalf("got {%d,%q}, want {%d,%q}", got.ExitCode, got.Message, tt.wantCode, tt.wantMsg)
			}
			if run != tt.wantRun {
				t.Fatalf("ran %d handlers, want %d (short-circuit broken)", run, tt.wantRun)
			}
		})
	}
}

// handler 收到的 payload 原样透传。
func TestHookRunner_HandlerReceivesPayload(t *testing.T) {
	r := NewHookRunner()
	var got map[string]any
	r.Register(HookPostToolUse, func(p map[string]any) HookResult {
		got = p
		return HookResult{}
	})
	r.Run(HookPostToolUse, map[string]any{"tool_name": "bash", "output": "ok"})

	if got["tool_name"] != "bash" || got["output"] != "ok" {
		t.Fatalf("payload not passed through: %+v", got)
	}
}

// 事件隔离：只触发匹配事件的 handler，不殃及其它事件。
func TestHookRunner_EventIsolation(t *testing.T) {
	r := NewHookRunner()
	pre, post := 0, 0
	r.Register(HookPreToolUse, func(map[string]any) HookResult { pre++; return HookResult{} })
	r.Register(HookPostToolUse, func(map[string]any) HookResult { post++; return HookResult{} })

	r.Run(HookPreToolUse, nil)
	if pre != 1 || post != 0 {
		t.Fatalf("event isolation broken: pre=%d post=%d", pre, post)
	}
}

// 注册顺序即执行顺序。
func TestHookRunner_PreservesOrder(t *testing.T) {
	r := NewHookRunner()
	var order []int
	for i := 0; i < 3; i++ {
		i := i
		r.Register(HookSessionStart, func(map[string]any) HookResult {
			order = append(order, i)
			return HookResult{}
		})
	}
	r.Run(HookSessionStart, nil)
	if len(order) != 3 || order[0] != 0 || order[1] != 1 || order[2] != 2 {
		t.Fatalf("handlers ran out of order: %v", order)
	}
}

// nil handler 被忽略，不影响后续判定。
func TestHookRunner_RegisterNilIgnored(t *testing.T) {
	r := NewHookRunner()
	r.Register(HookSessionStart, nil)
	if got := r.Run(HookSessionStart, nil); got.ExitCode != HookContinue {
		t.Fatalf("nil handler should be ignored, got exit=%d", got.ExitCode)
	}
}
