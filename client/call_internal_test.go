package client

import (
	"strings"
	"testing"
)

func TestErrorBodySnippet(t *testing.T) {
	// 折叠空白成单行
	if got := errorBodySnippet([]byte("line1\n  line2\t line3")); got != "line1 line2 line3" {
		t.Fatalf("whitespace not collapsed: %q", got)
	}
	// 空体有兜底文案
	if got := errorBodySnippet([]byte("   \n  ")); got != "(empty error body)" {
		t.Fatalf("empty body fallback = %q", got)
	}
	// 超长截断并加省略号
	long := strings.Repeat("x", 500)
	got := errorBodySnippet([]byte(long))
	if !strings.HasSuffix(got, "…") || len([]rune(got)) != 301 {
		t.Fatalf("long body not truncated to 300+…, got len=%d", len([]rune(got)))
	}
}
