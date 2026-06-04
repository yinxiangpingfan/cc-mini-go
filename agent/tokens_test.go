package agent

import (
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools"
	"github.com/yinxiangpingfan/cc-mini-go/config"
)

// 没配 max_context_tokens 时回退默认；配了用配的。
func TestContextTokenBudget(t *testing.T) {
	if got := (config.Config{}).ContextTokenBudget(); got != config.DefaultMaxContextTokens {
		t.Fatalf("unset budget = %d, want default %d", got, config.DefaultMaxContextTokens)
	}
	if got := (config.Config{MaxContextTokens: 12345}).ContextTokenBudget(); got != 12345 {
		t.Fatalf("configured budget = %d, want 12345", got)
	}
}

// EstimateTokens：空为 0，内容越多估得越大。
func TestEstimateTokens(t *testing.T) {
	if got := agent_tools.EstimateTokens(nil); got != 0 {
		t.Fatalf("empty estimate = %d, want 0", got)
	}
	small := []any{map[string]string{"role": "user", "content": "hi"}}
	large := []any{map[string]string{"role": "user", "content": "hi"}, map[string]string{"role": "assistant", "content": "this is a much longer reply with many more tokens"}}
	if agent_tools.EstimateTokens(small) >= agent_tools.EstimateTokens(large) {
		t.Fatal("more content should estimate to more tokens")
	}
}

// estimateContextTokens：有 API 基线时用「基线 + 尾巴」；无基线时用「全量估算 + 冷启动开销」。
func TestEstimateContextTokens_BaselineVsColdStart(t *testing.T) {
	a := &ChatCompletionAgent{}
	msgs := []any{
		map[string]string{"role": "user", "content": "first"},
		map[string]string{"role": "assistant", "content": "second"},
		map[string]string{"role": "tool", "content": "third"},
	}

	// 冷启动：无基线（lastPromptTokens=0）→ 全量估算 + 固定开销
	a.lastPromptTokens = 0
	cold := a.estimateContextTokens(msgs, 0)
	wantCold := agent_tools.EstimateTokens(msgs) + coldStartOverheadTokens
	if cold != wantCold {
		t.Fatalf("cold-start = %d, want %d", cold, wantCold)
	}

	// 权威路径：基线 5000 + 尾巴（第 2 条之后新增的）估算
	a.lastPromptTokens = 5000
	got := a.estimateContextTokens(msgs, 2)
	wantTail := agent_tools.EstimateTokens(msgs[2:])
	if got != 5000+wantTail {
		t.Fatalf("baseline+tail = %d, want %d", got, 5000+wantTail)
	}

	// sentCount==0 即便有基线也走冷启动路径（新一次调用，边界对不上，避免双算）
	if a.estimateContextTokens(msgs, 0) != wantCold {
		t.Fatal("sentCount==0 must fall back to cold-start path even with a baseline")
	}

	// sentCount 越界（压缩后历史变短）也安全回退，不 panic
	if a.estimateContextTokens(msgs, 99) != wantCold {
		t.Fatal("out-of-range sentCount must fall back safely")
	}
}
