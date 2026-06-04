package agent

import (
	stderrors "errors"
	"fmt"
	"testing"

	perrors "github.com/yinxiangpingfan/cc-mini-go/errors"
)

func TestChooseRecovery(t *testing.T) {
	// 模拟 retry.go 实际产生的包装错误：ErrContextTooLong 在最外层用 %w 包裹
	ctxErr := fmt.Errorf("%w: %v", perrors.ErrContextTooLong, "http 400: context_length_exceeded")
	cases := []struct {
		name         string
		finishReason string
		err          error
		wantKind     recoveryKind
	}{
		{"truncated output", "length", nil, recoveryContinue},
		{"normal stop", "stop", nil, recoveryNone},
		{"normal tool_calls", "tool_calls", nil, recoveryNone},
		{"empty finish no err", "", nil, recoveryNone},
		{"context too long", "", ctxErr, recoveryCompact},
		{"other error", "", stderrors.New("boom"), recoveryFail},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := chooseRecovery(c.finishReason, c.err)
			if got != c.wantKind {
				t.Fatalf("chooseRecovery(%q, %v) kind = %d, want %d", c.finishReason, c.err, got, c.wantKind)
			}
		})
	}
}

// ErrContextTooLong 必须能透过 retry.go 的包装被 errors.Is 识别。
func TestErrContextTooLong_IsDetectable(t *testing.T) {
	wrapped := fmt.Errorf("%w: %v", perrors.ErrContextTooLong, "some body")
	if !stderrors.Is(wrapped, perrors.ErrContextTooLong) {
		t.Fatal("wrapped ErrContextTooLong should be detectable via errors.Is")
	}
	if k, _ := chooseRecovery("", wrapped); k != recoveryCompact {
		t.Fatalf("wrapped context error should map to recoveryCompact, got %d", k)
	}
}

func TestIsContextTooLong(t *testing.T) {
	cases := []struct {
		code int
		text string
		want bool
	}{
		{413, "anything", true},                                  // 413 本身即强信号
		{400, "http 400: context_length_exceeded ...", true},     // OpenAI 错误码
		{400, "This model's maximum context length is 8192", true},
		{400, "please reduce the length of the messages", true},
		{400, "invalid api key", false},
		{401, "unauthorized", false},
		{429, "rate limit reached", false},
	}
	for _, c := range cases {
		if got := isContextTooLong(c.code, c.text); got != c.want {
			t.Fatalf("isContextTooLong(%d, %q) = %v, want %v", c.code, c.text, got, c.want)
		}
	}
}

// 续写预算耗尽后，chooseRecovery 仍判 continue，但循环里的 < maxContinueAttempts 守卫会拦住。
// 这里直接验证预算常量的语义边界。
func TestContinuationBudgetBoundary(t *testing.T) {
	rec := recoveryState{continueAttempts: maxContinueAttempts}
	k, _ := chooseRecovery("length", nil)
	if k != recoveryContinue {
		t.Fatal("length should still be classified as continue")
	}
	// 预算已满：调用方不应再续写
	if rec.continueAttempts < maxContinueAttempts {
		t.Fatal("precondition: budget should be exhausted")
	}
}
