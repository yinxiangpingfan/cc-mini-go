package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/agent/core"
)

// 未配置权限引擎时，gateToolCall 一律放行——保持原有行为，零侵入。
func TestGateToolCall_NoEngineAllows(t *testing.T) {
	a := &ChatCompletionAgent{}
	if res, blocked := a.gateToolCall(context.Background(), "id1", "bash", `{}`, map[string]any{"command": "rm -rf /"}); blocked {
		t.Fatalf("nil engine should never block, got blocked with %q", res)
	}
}

// deny 判定：拦截且返回一条带 error 字段的 JSON 结果。
func TestGateToolCall_DenyBlocksWithPairedResult(t *testing.T) {
	eng := core.NewPermissionEngine(core.ModeDefault,
		[]core.PermissionRule{{Tool: "bash", Content: "sudo *", Behavior: core.PermDeny}}, nil)
	a := &ChatCompletionAgent{perms: eng}

	res, blocked := a.gateToolCall(context.Background(), "id1", "bash", `{"command":"sudo reboot"}`,
		map[string]any{"command": "sudo reboot"})
	if !blocked {
		t.Fatal("deny rule should block the call")
	}
	assertErrorJSON(t, res, "permission denied")
}

// ask 判定 + 审批回调返回 true：放行。
func TestGateToolCall_AskApproved(t *testing.T) {
	eng := core.NewPermissionEngine(core.ModeDefault, nil, nil)
	var gotReq PermissionRequest
	a := &ChatCompletionAgent{
		perms: eng,
		approve: func(_ context.Context, req PermissionRequest) bool {
			gotReq = req
			return true
		},
	}
	res, blocked := a.gateToolCall(context.Background(), "id7", "bash", `{"command":"ls"}`,
		map[string]any{"command": "ls"})
	if blocked {
		t.Fatalf("approved ask should proceed, got blocked with %q", res)
	}
	if gotReq.ToolID != "id7" || gotReq.ToolName != "bash" || gotReq.Args["command"] != "ls" {
		t.Fatalf("approval callback received wrong request: %+v", gotReq)
	}
	if gotReq.Reason == "" {
		t.Fatal("approval request should carry a non-empty reason")
	}
}

// ask 判定 + 审批回调返回 false：拒绝，返回配对结果。
func TestGateToolCall_AskRejected(t *testing.T) {
	eng := core.NewPermissionEngine(core.ModeDefault, nil, nil)
	a := &ChatCompletionAgent{
		perms:   eng,
		approve: func(_ context.Context, _ PermissionRequest) bool { return false },
	}
	res, blocked := a.gateToolCall(context.Background(), "id8", "bash", `{}`, map[string]any{"command": "ls"})
	if !blocked {
		t.Fatal("rejected ask should block")
	}
	assertErrorJSON(t, res, "denied by user")
}

// ask 判定但未配置审批回调：fail-closed，按拒绝处理。
func TestGateToolCall_AskNoApproverFailsClosed(t *testing.T) {
	eng := core.NewPermissionEngine(core.ModeDefault, nil, nil)
	a := &ChatCompletionAgent{perms: eng}
	res, blocked := a.gateToolCall(context.Background(), "id9", "bash", `{}`, map[string]any{"command": "ls"})
	if !blocked {
		t.Fatal("ask without approver should fail closed (block)")
	}
	assertErrorJSON(t, res, "denied by user")
}

// allow 判定（auto 模式下的只读工具）：放行。
func TestGateToolCall_AllowProceeds(t *testing.T) {
	eng := core.NewPermissionEngine(core.ModeAuto, nil, nil)
	a := &ChatCompletionAgent{perms: eng}
	if res, blocked := a.gateToolCall(context.Background(), "id2", "read_file", `{}`,
		map[string]any{"file_path": "/tmp/x"}); blocked {
		t.Fatalf("auto mode should allow read_file, got blocked with %q", res)
	}
}

// 连续 3 次被拒后发出且仅发出一次提示事件。
func TestRecordDeny_EmitsHintAfterThreshold(t *testing.T) {
	ch := make(chan AgentEvent, 16)
	eng := core.NewPermissionEngine(core.ModeDefault,
		[]core.PermissionRule{{Tool: "bash", Behavior: core.PermDeny}}, nil)
	a := &ChatCompletionAgent{perms: eng, events: ch}

	args := map[string]any{"command": "ls"}
	for i := 0; i < consecutiveDenyHint; i++ {
		if _, blocked := a.gateToolCall(context.Background(), "id", "bash", "{}", args); !blocked {
			t.Fatal("bash deny rule should block")
		}
	}
	if n := countHints(ch); n != 1 {
		t.Fatalf("want exactly 1 hint after %d denials, got %d", consecutiveDenyHint, n)
	}
}

// 任一放行打断连续计数：deny、deny、allow、deny、deny 不应触发提示。
func TestRecordDeny_AllowResetsStreak(t *testing.T) {
	ch := make(chan AgentEvent, 16)
	eng := core.NewPermissionEngine(core.ModeAuto, // auto 下 read_file 自动放行
		[]core.PermissionRule{{Tool: "bash", Behavior: core.PermDeny}}, nil)
	a := &ChatCompletionAgent{perms: eng, events: ch}
	bash := map[string]any{"command": "ls"}
	read := map[string]any{"file_path": "/x"}

	a.gateToolCall(context.Background(), "id", "bash", "{}", bash)
	a.gateToolCall(context.Background(), "id", "bash", "{}", bash)
	a.gateToolCall(context.Background(), "id", "read_file", "{}", read) // 放行 → 清零
	a.gateToolCall(context.Background(), "id", "bash", "{}", bash)
	a.gateToolCall(context.Background(), "id", "bash", "{}", bash)
	if n := countHints(ch); n != 0 {
		t.Fatalf("allow should reset streak, expected 0 hints, got %d", n)
	}
}

// countHints 非阻塞地数出 channel 里的 EventPermissionHint 数量。
func countHints(ch chan AgentEvent) int {
	n := 0
	for {
		select {
		case ev := <-ch:
			if ev.Type == EventPermissionHint {
				n++
			}
		default:
			return n
		}
	}
}

// 拦截结果必须是合法 JSON 且含 error 字段，含期望子串——确保能作为 tool 消息回传给模型。
func assertErrorJSON(t *testing.T, res, wantSub string) {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal([]byte(res), &m); err != nil {
		t.Fatalf("denied result is not valid JSON: %v (%q)", err, res)
	}
	if _, ok := m["error"]; !ok {
		t.Fatalf("denied result should carry an 'error' field: %q", res)
	}
	if !strings.Contains(m["error"], wantSub) {
		t.Fatalf("denied result %q should contain %q", res, wantSub)
	}
}
