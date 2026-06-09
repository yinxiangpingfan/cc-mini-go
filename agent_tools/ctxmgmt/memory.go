package ctxmgmt

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

// memoryFileExt 是单条 memory 文件的扩展名：每条记忆落一个 <name>.md。
const memoryFileExt = ".md"

// memoryIndexName 是人类可读的索引文件名。它不参与加载（扫描时跳过），仅供浏览。
const memoryIndexName = "MEMORY.md"

// MemoryTypes 是允许的 4 类记忆（教学 s09 的存储边界）：
//   - user      用户长期偏好（风格、详略、工具链）
//   - feedback  用户明确纠正过、或确认有效的做法（含「为什么」）
//   - project   不易从代码直接看出的项目约定/背景（如某决定出于合规）
//   - reference 外部资源指针（看板、监控面板、URL）
//
// 注意边界：代码结构、当前任务进度、分支名/PR 号、bug 修复细节、密钥都不该进 memory，
// 这条边界写在 save_memory 工具的描述里，由模型遵守（见 prompt.SaveMemoryPrompt）。
var MemoryTypes = []string{"user", "feedback", "project", "reference"}

// isValidMemoryType 校验 type 是否在白名单内。
func isValidMemoryType(t string) bool {
	for _, v := range MemoryTypes {
		if v == t {
			return true
		}
	}
	return false
}

// MemoryEntry 是一条记忆：frontmatter 元信息 + 正文。
type MemoryEntry struct {
	Name        string
	Description string
	Type        string
	Body        string
	Path        string
}

// MemoryStore 管理所有跨会话记忆。它是 SkillRegistry 的镜像，但方向相反：
// skill 只读、正文按需 load_skill 加载；memory 可读可写、正文在会话开始即全部注入 prompt。
//
// 与 hook 不同，这里的锁是真需要的：save_memory/delete_memory 会在工具 goroutine
// 并发执行期间写盘、重建索引、改 entries，与 global.go 的 ReadFiles 同源。
type MemoryStore struct {
	writeDir string                  // 新记忆落盘目录（取扫描目录里最靠后的那个，即项目级）
	dirs     []string                // 扫描目录：全局 + 项目（后者同名覆盖前者）
	entries  map[string]MemoryEntry  // name -> 记忆
	mu       sync.RWMutex
}

// NewMemoryStore 从给定目录加载记忆。dirs 按顺序加载，后面的同名覆盖前面的；
// 最后一个目录作为新记忆的写入目录。不存在的目录会被静默跳过。
func NewMemoryStore(dirs ...string) *MemoryStore {
	s := &MemoryStore{
		dirs:    dirs,
		entries: make(map[string]MemoryEntry),
	}
	if len(dirs) > 0 {
		s.writeDir = dirs[len(dirs)-1]
	}
	s.Reload()
	return s
}

// Reload 重新扫描所有目录，重建内存表。
func (s *MemoryStore) Reload() {
	entries := make(map[string]MemoryEntry)
	for _, dir := range s.dirs {
		loadMemoryDir(dir, entries)
	}
	s.mu.Lock()
	s.entries = entries
	s.mu.Unlock()
}

// loadMemoryDir 扫描 dir 下的 *.md（跳过 MEMORY.md 索引）并注册进 into。非递归：记忆是扁平文件。
func loadMemoryDir(dir string, into map[string]MemoryEntry) {
	if dir == "" {
		return
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, f := range files {
		if f.IsDir() || f.Name() == memoryIndexName || !strings.HasSuffix(f.Name(), memoryFileExt) {
			continue
		}
		path := filepath.Join(dir, f.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		meta, body := parseFrontmatter(string(raw))
		name := meta["name"]
		if name == "" {
			name = strings.TrimSuffix(f.Name(), memoryFileExt)
		}
		typ := meta["type"]
		if !isValidMemoryType(typ) {
			typ = "project" // 非法/缺失 type 兜底归类，避免整条记忆被忽略
		}
		desc := meta["description"]
		if desc == "" {
			desc = "No description"
		}
		into[name] = MemoryEntry{
			Name:        name,
			Description: desc,
			Type:        typ,
			Body:        strings.TrimSpace(body),
			Path:        path,
		}
	}
}

// Describe 返回注入 system prompt 的记忆段落（读取层）。
// 与 skill 不同：记忆很小且是「长期方向」，所以正文全部注入，无需按需加载。
// 按 type 分组、组内按 name 排序，保证输出稳定。无记忆时返回空串（调用方据此决定是否注入）。
func (s *MemoryStore) Describe() string {
	s.mu.RLock()
	s.mu.RUnlock()
	if len(s.entries) == 0 {
		return ""
	}
	byType := make(map[string][]MemoryEntry)
	for _, e := range s.entries {
		byType[e.Type] = append(byType[e.Type], e)
	}
	var b strings.Builder
	for _, typ := range MemoryTypes {
		group := byType[typ]
		if len(group) == 0 {
			continue
		}
		sort.Slice(group, func(i, j int) bool { return group[i].Name < group[j].Name })
		b.WriteString("[" + typ + "]\n")
		for _, e := range group {
			b.WriteString(fmt.Sprintf("- %s: %s\n", e.Name, e.Body))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// Save 校验并写入一条记忆：写 <name>.md（带 frontmatter）、重建 MEMORY.md 索引、更新内存表。
// 在工具 goroutine 并发期间调用，故写盘与改表全程持锁。返回落盘后的记忆。
func (s *MemoryStore) Save(name, description, typ, body string) (MemoryEntry, error) {
	typ = strings.TrimSpace(typ)
	if !isValidMemoryType(typ) {
		return MemoryEntry{}, fmt.Errorf(errors.ErrMemoryInvalidType, typ, strings.Join(MemoryTypes, ", "))
	}
	safe := sanitizeMemoryName(name)
	if safe == "" {
		return MemoryEntry{}, errors.ErrMemoryEmptyName
	}
	if strings.TrimSpace(description) == "" {
		description = "No description"
	}

	dir := s.resolveWriteDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return MemoryEntry{}, fmt.Errorf("%w: %w", errors.ErrMemoryWrite, err)
	}
	path := filepath.Join(dir, safe+memoryFileExt)
	entry := MemoryEntry{
		Name:        safe,
		Description: strings.TrimSpace(description),
		Type:        typ,
		Body:        strings.TrimSpace(body),
		Path:        path,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.WriteFile(path, []byte(renderMemoryFile(entry)), 0o644); err != nil {
		return MemoryEntry{}, fmt.Errorf("%w: %w", errors.ErrMemoryWrite, err)
	}
	s.entries[safe] = entry
	s.writeIndexLocked(dir)
	return entry, nil
}

// Delete 删除一条记忆文件并更新索引与内存表。找不到时返回错误（含已知列表，便于纠正）。
func (s *MemoryStore) Delete(name string) error {
	safe := sanitizeMemoryName(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[safe]
	if !ok {
		return fmt.Errorf(errors.ErrMemoryNotExist, name, s.knownNamesLocked())
	}
	if err := os.Remove(entry.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%w: %w", errors.ErrMemoryDelete, err)
	}
	delete(s.entries, safe)
	s.writeIndexLocked(filepath.Dir(entry.Path))
	return nil
}

// resolveWriteDir 返回新记忆的落盘目录；writeDir 为空时兜底到 <cwd>/.cc_mini_go/memory。
func (s *MemoryStore) resolveWriteDir() string {
	if s.writeDir != "" {
		return s.writeDir
	}
	if cwd, err := os.Getwd(); err == nil {
		return filepath.Join(cwd, ".cc_mini_go", "memory")
	}
	return ".cc_mini_go/memory"
}

// writeIndexLocked 在 dir 下重建 MEMORY.md 索引（仅含落在该目录的记忆）。调用方须已持锁。
// 索引失败不致命（仅影响人类浏览），故忽略写错误。
func (s *MemoryStore) writeIndexLocked(dir string) {
	rows := make([]string, 0, len(s.entries))
	for _, e := range s.entries {
		if filepath.Dir(e.Path) != dir {
			continue
		}
		rows = append(rows, fmt.Sprintf("- %s: %s [%s]", e.Name, e.Description, e.Type))
	}
	sort.Strings(rows)
	var b strings.Builder
	b.WriteString("# Memory Index\n\n")
	for _, r := range rows {
		b.WriteString(r + "\n")
	}
	_ = os.WriteFile(filepath.Join(dir, memoryIndexName), []byte(b.String()), 0o644)
}

// knownNamesLocked 返回已知记忆名（逗号分隔），供错误提示。调用方须已持锁。
func (s *MemoryStore) knownNamesLocked() string {
	if len(s.entries) == 0 {
		return "(none)"
	}
	names := make([]string, 0, len(s.entries))
	for n := range s.entries {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// renderMemoryFile 把一条记忆渲染成带 frontmatter 的文件内容（与 parseFrontmatter 互逆）。
func renderMemoryFile(e MemoryEntry) string {
	return fmt.Sprintf("---\nname: %s\ndescription: %s\ntype: %s\n---\n%s\n",
		e.Name, e.Description, e.Type, e.Body)
}

// sanitizeMemoryName 把名字清洗成安全的文件名：只保留字母/数字/-/_，其余替换为 '-'，
// 并裁掉首尾的 '-'。这同时挡住了路径穿越（如 ../、绝对路径）。
func sanitizeMemoryName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// DefaultMemoryDirs 返回默认记忆目录：先全局，后项目（项目覆盖全局，新记忆写项目）。
// 全局：~/.cc_mini_go/memory    项目：<cwd>/.cc_mini_go/memory
func DefaultMemoryDirs() []string {
	dirs := make([]string, 0, 2)
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".cc_mini_go", "memory"))
	}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, filepath.Join(cwd, ".cc_mini_go", "memory"))
	}
	return dirs
}

// Memory 是包级默认记忆库，从全局 + 项目目录加载。
var Memory = NewMemoryStore(DefaultMemoryDirs()...)

// NewSaveMemoryTool 返回 save_memory 工具：把一条跨会话记忆写盘。
func NewSaveMemoryTool(store *MemoryStore) *Tools {
	return &Tools{
		Name: "save_memory",
		Func: func(ctx context.Context, args map[string]any) string {
			name, ok := args["name"].(string)
			if !ok || strings.TrimSpace(name) == "" {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "name"))
			}
			content, ok := args["content"].(string)
			if !ok || strings.TrimSpace(content) == "" {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "content"))
			}
			typ, ok := args["type"].(string)
			if !ok || typ == "" {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "type"))
			}
			description, _ := args["description"].(string)
			entry, err := store.Save(name, description, typ, content)
			if err != nil {
				return shared.JsonErr(err.Error())
			}
			b, _ := json.Marshal(map[string]string{
				"saved": entry.Name,
				"type":  entry.Type,
				"path":  entry.Path,
			})
			return string(b)
		},
	}
}

func (t *Tools) SaveMemoryInfoForLLM() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "save_memory",
			Description: prompt.SaveMemoryPrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"name": client.ParameterProperty{
						Type:        "string",
						Description: "Short kebab-case identifier, e.g. 'prefer-tabs'. Reusing an existing name overwrites that memory.",
					},
					"description": client.ParameterProperty{
						Type:        "string",
						Description: "One-line summary of the memory (shown in the index).",
					},
					"type": client.ParameterProperty{
						Type:        "string",
						Description: "One of: user, feedback, project, reference.",
						Enum:        MemoryTypes,
					},
					"content": client.ParameterProperty{
						Type:        "string",
						Description: "The fact to remember, in full sentences.",
					},
				},
				Required: []string{"name", "type", "content"},
			},
		},
	}
}

// NewDeleteMemoryTool 返回 delete_memory 工具：按名删除一条过时/错误的记忆。
func NewDeleteMemoryTool(store *MemoryStore) *Tools {
	return &Tools{
		Name: "delete_memory",
		Func: func(ctx context.Context, args map[string]any) string {
			name, ok := args["name"].(string)
			if !ok || strings.TrimSpace(name) == "" {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "name"))
			}
			if err := store.Delete(name); err != nil {
				return shared.JsonErr(err.Error())
			}
			b, _ := json.Marshal(map[string]string{"deleted": sanitizeMemoryName(name)})
			return string(b)
		},
	}
}

func (t *Tools) DeleteMemoryInfoForLLM() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "delete_memory",
			Description: prompt.DeleteMemoryPrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"name": client.ParameterProperty{
						Type:        "string",
						Description: "The exact name of the memory to delete, as shown in the memory section.",
					},
				},
				Required: []string{"name"},
			},
		},
	}
}
