package ctxmgmt

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSkill 在 dir/<skillName>/SKILL.md 写入一份 skill 文件，返回其路径。
func writeSkill(t *testing.T, dir, skillName, content string) {
	t.Helper()
	skillDir := filepath.Join(dir, skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", skillDir, err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, skillFileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

// ---------- frontmatter 解析 ----------

func TestParseFrontmatter_Basic(t *testing.T) {
	text := "---\nname: code-review\ndescription: Review checklist\n---\nThis is the body."
	meta, body := parseFrontmatter(text)

	if meta["name"] != "code-review" {
		t.Errorf("expected name 'code-review', got: %q", meta["name"])
	}
	if meta["description"] != "Review checklist" {
		t.Errorf("expected description 'Review checklist', got: %q", meta["description"])
	}
	if body != "This is the body." {
		t.Errorf("expected body 'This is the body.', got: %q", body)
	}
}

func TestParseFrontmatter_CRLF(t *testing.T) {
	text := "---\r\nname: git\r\ndescription: Git guide\r\n---\r\nBody line."
	meta, body := parseFrontmatter(text)

	if meta["name"] != "git" {
		t.Errorf("expected name 'git', got: %q", meta["name"])
	}
	if !strings.Contains(body, "Body line.") {
		t.Errorf("expected body to contain 'Body line.', got: %q", body)
	}
}

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	text := "just a plain body with no frontmatter"
	meta, body := parseFrontmatter(text)

	if len(meta) != 0 {
		t.Errorf("expected empty meta, got: %v", meta)
	}
	if body != text {
		t.Errorf("expected body unchanged, got: %q", body)
	}
}

func TestParseFrontmatter_BlockScalar(t *testing.T) {
	text := "---\nname: stats\ndescription: |\n  First line of description.\n  Second line.\n\n  After a blank line.\nother: tail\n---\nbody here"
	meta, body := parseFrontmatter(text)

	if meta["name"] != "stats" {
		t.Errorf("expected name 'stats', got: %q", meta["name"])
	}
	// 块标量应折叠成单行，不能是 "|"
	want := "First line of description. Second line. After a blank line."
	if meta["description"] != want {
		t.Errorf("expected folded description %q, got: %q", want, meta["description"])
	}
	// 块标量后的非缩进字段应被正确解析
	if meta["other"] != "tail" {
		t.Errorf("expected field after block scalar 'tail', got: %q", meta["other"])
	}
	if body != "body here" {
		t.Errorf("expected body 'body here', got: %q", body)
	}
}

func TestParseFrontmatter_BlankLineAfterFence(t *testing.T) {
	text := "---\nname: x\n---\n\nBody after blank line."
	_, body := parseFrontmatter(text)
	if body != "Body after blank line." {
		t.Errorf("expected leading blank line trimmed, got: %q", body)
	}
}

// ---------- 注册表加载 ----------

func TestSkillRegistry_LoadsSkills(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "code-review", "---\nname: code-review\ndescription: Review checklist\n---\nReview steps here.")
	writeSkill(t, dir, "git-workflow", "---\nname: git-workflow\ndescription: Commit guidance\n---\nGit steps here.")

	r := NewSkillRegistry(dir)
	if len(r.Documents) != 2 {
		t.Fatalf("expected 2 skills, got: %d", len(r.Documents))
	}
	if r.Documents["code-review"].Body != "Review steps here." {
		t.Errorf("unexpected body: %q", r.Documents["code-review"].Body)
	}
}

func TestSkillRegistry_MissingDirIsSafe(t *testing.T) {
	r := NewSkillRegistry(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(r.Documents) != 0 {
		t.Fatalf("expected 0 skills for missing dir, got: %d", len(r.Documents))
	}
}

func TestSkillRegistry_NameFallsBackToDir(t *testing.T) {
	dir := t.TempDir()
	// 没有 name 字段，应回退为目录名
	writeSkill(t, dir, "my-skill", "---\ndescription: no name field\n---\nbody")

	r := NewSkillRegistry(dir)
	if _, ok := r.Documents["my-skill"]; !ok {
		t.Fatalf("expected skill keyed by dir name 'my-skill', got keys: %v", keysOf(r.Documents))
	}
}

func TestSkillRegistry_ProjectOverridesGlobal(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()
	writeSkill(t, globalDir, "code-review", "---\nname: code-review\ndescription: GLOBAL\n---\nglobal body")
	writeSkill(t, projectDir, "code-review", "---\nname: code-review\ndescription: PROJECT\n---\nproject body")

	// 全局先加载，项目后加载 → 项目覆盖全局
	r := NewSkillRegistry(globalDir, projectDir)
	if len(r.Documents) != 1 {
		t.Fatalf("expected 1 skill after merge, got: %d", len(r.Documents))
	}
	if r.Documents["code-review"].Manifest.Description != "PROJECT" {
		t.Fatalf("expected project to override global, got: %q", r.Documents["code-review"].Manifest.Description)
	}
}

// ---------- 发现层 DescribeAvailable ----------

func TestDescribeAvailable_Empty(t *testing.T) {
	r := NewSkillRegistry()
	if got := r.DescribeAvailable(); got != "(no skills available)" {
		t.Fatalf("expected '(no skills available)', got: %q", got)
	}
}

func TestDescribeAvailable_SortedCatalog(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "zebra", "---\nname: zebra\ndescription: Z skill\n---\nbody")
	writeSkill(t, dir, "alpha", "---\nname: alpha\ndescription: A skill\n---\nbody")

	r := NewSkillRegistry(dir)
	got := r.DescribeAvailable()

	// 应按名称排序：alpha 在 zebra 前
	if strings.Index(got, "alpha") > strings.Index(got, "zebra") {
		t.Fatalf("expected sorted catalog (alpha before zebra), got:\n%s", got)
	}
	if !strings.Contains(got, "- alpha: A skill") {
		t.Fatalf("expected catalog line for alpha, got:\n%s", got)
	}
	// 目录只含名称与描述，不应含正文
	if strings.Contains(got, "body") {
		t.Fatalf("catalog must not contain skill body, got:\n%s", got)
	}
}

// ---------- 加载层 LoadFullText ----------

func TestLoadFullText_WrapsInSkillTag(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "code-review", "---\nname: code-review\ndescription: d\n---\nThe full review checklist.")

	r := NewSkillRegistry(dir)
	got := r.LoadFullText("code-review")

	if !strings.HasPrefix(got, `<skill name="code-review">`) {
		t.Fatalf("expected <skill> wrapper, got:\n%s", got)
	}
	if !strings.Contains(got, "The full review checklist.") {
		t.Fatalf("expected body in output, got:\n%s", got)
	}
	if !strings.HasSuffix(got, "</skill>") {
		t.Fatalf("expected closing </skill>, got:\n%s", got)
	}
}

func TestLoadFullText_UnknownSkill(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "known", "---\nname: known\ndescription: d\n---\nbody")

	r := NewSkillRegistry(dir)
	got := r.LoadFullText("nonexistent")

	var resp map[string]string
	if err := json.Unmarshal([]byte(got), &resp); err != nil {
		t.Fatalf("expected JSON error, got non-JSON: %s", got)
	}
	if !strings.Contains(resp["error"], "unknown skill") {
		t.Fatalf("expected 'unknown skill' error, got: %s", resp["error"])
	}
	// 错误里应列出已知 skill，便于模型纠正
	if !strings.Contains(resp["error"], "known") {
		t.Fatalf("expected available skills listed, got: %s", resp["error"])
	}
}

// ---------- load_skill 工具入口 ----------

func TestNewLoadSkillTool_MissingName(t *testing.T) {
	r := NewSkillRegistry()
	tool := NewLoadSkillTool(r)
	out := tool.Func(context.Background(), map[string]any{})

	var resp map[string]string
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("output is not valid JSON: %v, raw: %s", err, out)
	}
	if _, ok := resp["error"]; !ok {
		t.Fatalf("expected error when 'name' is missing, got: %s", out)
	}
}

func TestNewLoadSkillTool_Name(t *testing.T) {
	tool := NewLoadSkillTool(NewSkillRegistry())
	if tool.Name != "load_skill" {
		t.Fatalf("expected tool name 'load_skill', got: %s", tool.Name)
	}
}

func TestNewLoadSkillTool_LoadsBody(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "mcp-builder", "---\nname: mcp-builder\ndescription: d\n---\nMCP build steps.")

	tool := NewLoadSkillTool(NewSkillRegistry(dir))
	out := tool.Func(context.Background(), map[string]any{"name": "mcp-builder"})

	if !strings.Contains(out, "MCP build steps.") {
		t.Fatalf("expected skill body, got: %s", out)
	}
	if !strings.Contains(out, `<skill name="mcp-builder">`) {
		t.Fatalf("expected <skill> wrapper, got: %s", out)
	}
}

// ---------- 工具 schema ----------

func TestLoadSkillInfoForLLM_Schema(t *testing.T) {
	var tool *Tools
	info := tool.LoadSkillInfoForLLM()

	if info.Function.Name != "load_skill" {
		t.Fatalf("expected function name 'load_skill', got: %s", info.Function.Name)
	}
	if len(info.Function.Parameters.Required) != 1 || info.Function.Parameters.Required[0] != "name" {
		t.Fatalf("expected required=['name'], got: %v", info.Function.Parameters.Required)
	}
	if _, ok := info.Function.Parameters.Properties["name"]; !ok {
		t.Fatalf("expected 'name' property, got: %v", info.Function.Parameters.Properties)
	}
}

// keysOf 返回 map 的键集合，仅用于测试错误信息。
func keysOf(m map[string]SkillDocument) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
