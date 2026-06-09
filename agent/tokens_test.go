package agent

import (
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/ctxmgmt"
	"github.com/yinxiangpingfan/cc-mini-go/config"
)

// 没配 max_context_tokens 时回退默认窗口（200K）；配了用配的。
func TestContextWindow(t *testing.T) {
	if got := (config.Config{}).ContextWindow(); got != config.DefaultMaxContextTokens {
		t.Fatalf("unset window = %d, want default %d", got, config.DefaultMaxContextTokens)
	}
	if config.DefaultMaxContextTokens != 200000 {
		t.Fatalf("default window = %d, want 200000", config.DefaultMaxContextTokens)
	}
	if got := (config.Config{MaxContextTokens: 128000}).ContextWindow(); got != 128000 {
		t.Fatalf("configured window = %d, want 128000", got)
	}
}

// 阈值公式：200K 窗口 → 180K 有效 → 167K 阈值；小窗口兜底为正。
func TestAutoCompactThreshold(t *testing.T) {
	if got := ctxmgmt.AutoCompactThreshold(200000); got != 167000 {
		t.Fatalf("200K window threshold = %d, want 167000", got)
	}
	// 极小窗口不能产出 ≤0 的阈值（否则每轮都压）
	if got := ctxmgmt.AutoCompactThreshold(1000); got < 1 {
		t.Fatalf("tiny window threshold = %d, must be >= 1", got)
	}
}

// EstimateTokens：空为 0，内容越多估得越大。
func TestEstimateTokens(t *testing.T) {
	if got := ctxmgmt.EstimateTokens(nil); got != 0 {
		t.Fatalf("empty estimate = %d, want 0", got)
	}
	small := []any{map[string]string{"role": "user", "content": "hi"}}
	large := []any{map[string]string{"role": "user", "content": "hi"}, map[string]string{"role": "assistant", "content": "this is a much longer reply with many more tokens"}}
	if ctxmgmt.EstimateTokens(small) >= ctxmgmt.EstimateTokens(large) {
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
	wantCold := ctxmgmt.EstimateTokens(msgs) + coldStartOverheadTokens
	if cold != wantCold {
		t.Fatalf("cold-start = %d, want %d", cold, wantCold)
	}

	// 权威路径：基线 5000 + 尾巴（第 2 条之后新增的）估算
	a.lastPromptTokens = 5000
	got := a.estimateContextTokens(msgs, 2)
	wantTail := ctxmgmt.EstimateTokens(msgs[2:])
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
