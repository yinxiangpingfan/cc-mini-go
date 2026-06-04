package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

// joinNonEmpty 丢弃空段、按顺序连接非空段。
func TestJoinNonEmpty(t *testing.T) {
	got := joinNonEmpty("\n\n", "a", "", "   ", "b")
	if got != "a\n\nb" {
		t.Fatalf("joinNonEmpty = %q, want %q", got, "a\n\nb")
	}
	if joinNonEmpty("\n\n") != "" {
		t.Fatal("no parts should yield empty string")
	}
}

// 核心段始终在最前；动态块在边界标记之下。
func TestBuildSystemPrompt_OrderAndBoundary(t *testing.T) {
	a := &ChatCompletionAgent{}
	out := a.buildSystemPrompt("CORE-IDENTITY")

	if !strings.HasPrefix(out, "CORE-IDENTITY") {
		t.Fatalf("core section must come first:\n%s", out)
	}
	// sectionDynamic 至少含日期，故边界标记必然出现
	bidx := strings.Index(out, prompt.DynamicContextHeader)
	if bidx < 0 {
		t.Fatalf("dynamic boundary marker missing:\n%s", out)
	}
	// 动态内容（date）必须在边界之后
	didx := strings.Index(out, "Today's date:")
	if didx < bidx {
		t.Fatalf("dynamic content must follow the boundary marker (boundary=%d, date=%d)", bidx, didx)
	}
}

// 空段（无 skills/memory/CLAUDE.md）不会平白塞入标题。
func TestBuildSystemPrompt_SkipsEmptySections(t *testing.T) {
	// 切到一个临时空目录，确保 <cwd>/CLAUDE.md 不存在
	tmp := t.TempDir()
	chdir(t, tmp)

	a := &ChatCompletionAgent{}
	out := a.buildSystemPrompt("CORE")

	if strings.Contains(out, prompt.ClaudeMDHeader) {
		t.Fatal("no CLAUDE.md should mean no CLAUDE.md header")
	}
	// memory 段：包级 Memory 可能从真实目录加载，故这里只断言「无则无标题」的逻辑由 section 决定，
	// 不对全局 Memory 做强假设；仅验证 sectionMemory 在空时返回空串。
	if a.sectionMemory() == "" && strings.Contains(out, prompt.MemoryHeader) {
		t.Fatal("empty memory section should not emit the memory header")
	}
}

// 分层 CLAUDE.md：项目级 <cwd>/CLAUDE.md 被读入并带 project 标签。
func TestSectionClaudeMD_LayersProject(t *testing.T) {
	tmp := t.TempDir()
	chdir(t, tmp)
	if err := os.WriteFile(filepath.Join(tmp, "CLAUDE.md"), []byte("PROJECT-RULES"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &ChatCompletionAgent{}
	got := a.sectionClaudeMD()
	if !strings.Contains(got, prompt.ClaudeMDHeader) {
		t.Fatalf("missing header:\n%s", got)
	}
	if !strings.Contains(got, "## project") || !strings.Contains(got, "PROJECT-RULES") {
		t.Fatalf("project layer not loaded:\n%s", got)
	}
}

// 动态段含日期与工作目录；配了 model/perms 时一并出现。
func TestSectionDynamic(t *testing.T) {
	a := &ChatCompletionAgent{}
	got := a.sectionDynamic()
	if !strings.Contains(got, "Today's date:") || !strings.Contains(got, "Working directory:") {
		t.Fatalf("dynamic section missing date/cwd:\n%s", got)
	}
}

// chdir 切到 dir 并在测试结束时还原（t.TempDir 之外的副作用要复原）。
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}
