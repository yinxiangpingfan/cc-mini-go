package agent_tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

// 上下文压缩：在不丢主线连续性的前提下，把活跃上下文重新腾出空间。
// 三层机制：
//   1. 大工具结果落盘 + 预览标记（PersistLargeOutput）
//   2. 旧工具结果微压缩成占位（MicroCompact）
//   3. 整体过长时生成连续性摘要（CompactHistory）

const (
	// ContextLimit 估算上下文字节数超过该阈值时触发完整压缩
	ContextLimit = 50000
	// KeepRecentToolResults 微压缩时保留最近 N 个工具结果的完整内容
	KeepRecentToolResults = 3
	// PersistThreshold 工具输出超过该字符数时落盘，只在上下文留预览
	PersistThreshold = 30000
	// PreviewChars 落盘后保留的预览字符数
	PreviewChars = 2000
	// microCompactMinLen 短于该长度的工具结果不值得压缩
	microCompactMinLen = 120
	// summarizeInputLimit 喂给摘要模型的对话 JSON 最大字符数
	summarizeInputLimit = 80000
	// recentFilesLimit 摘要里附带的最近文件数量上限
	recentFilesLimit = 5
	// CompactToolName 手动压缩工具名
	CompactToolName = "compact"

	persistedPlaceholder = "[Earlier tool result compacted. Re-run the tool if you need full detail.]"
)

// sessionStorageDir 是本次会话的存储根，进程启动时确定一次，整个会话共用。
// 布局：~/.cc_mini_go/projects/<项目>/<会话>/ —— 按项目、按会话分别隔离。
// 落盘的大工具结果与转录都放在它下面；测试可覆盖该变量以隔离。
var sessionStorageDir = defaultSessionStorageDir()

// toolResultsDir 大工具结果落盘目录（会话级）
func toolResultsDir() string { return filepath.Join(sessionStorageDir, "tool-results") }

// transcriptDir 完整对话转录目录（会话级）
func transcriptDir() string { return filepath.Join(sessionStorageDir, "transcripts") }

// defaultSessionStorageDir 计算 ~/.cc_mini_go/projects/<项目>/<会话> 绝对路径。
// 取不到 home 时退回当前目录下的 .cc_mini_go，保证始终可写。
func defaultSessionStorageDir() string {
	base := ".cc_mini_go"
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		base = filepath.Join(home, ".cc_mini_go")
	}
	return filepath.Join(base, "projects", projectSlug(), newSessionID())
}

// projectSlug 把当前工作目录的绝对路径转成文件名安全的项目标识，
// 用路径分隔符替换保证不同项目不会撞名（如 -Users-foo-bar）。
func projectSlug() string {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		return "unknown"
	}
	slug := strings.ReplaceAll(cwd, string(os.PathSeparator), "-")
	slug = strings.ReplaceAll(slug, ":", "-") // 兼容 Windows 盘符
	if slug == "" {
		return "unknown"
	}
	return slug
}

// newSessionID 生成本次会话的唯一标识（时间戳 + 纳秒，便于排序与去重）。
func newSessionID() string {
	now := time.Now()
	return fmt.Sprintf("%s-%09d", now.Format("20060102-150405"), now.UnixNano()%1e9)
}

// CompactState 显式维护一份压缩状态，跨轮追踪是否已压缩及最近一次摘要。
type CompactState struct {
	HasCompacted bool
	LastSummary  string
}

// NewCompactState 创建一份初始压缩状态。
func NewCompactState() *CompactState {
	return &CompactState{}
}

// PersistLargeOutput 把过大的工具输出落盘，只在上下文里留一个带预览的标记。
// 小于阈值的输出原样返回。让模型知道「发生了什么」，但不强迫它一直背着整份大输出。
func PersistLargeOutput(toolID string, output string) string {
	if len(output) <= PersistThreshold {
		return output
	}
	dir := toolResultsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return output // 落盘失败则退回原文，不影响主流程
	}
	id := toolID
	if id == "" {
		id = fmt.Sprintf("tool-%d", time.Now().UnixNano())
	}
	storedPath := filepath.Join(dir, id+".txt")
	if _, err := os.Stat(storedPath); err != nil {
		if err := os.WriteFile(storedPath, []byte(output), 0644); err != nil {
			return output
		}
	}
	// 按 rune 截取预览，避免在多字节字符（如中文）中间切断产生非法 UTF-8
	preview := output
	if r := []rune(output); len(r) > PreviewChars {
		preview = string(r[:PreviewChars])
	}
	return fmt.Sprintf(
		"<persisted-output>\nFull output saved to: %s\nPreview:\n%s\n</persisted-output>",
		storedPath, preview,
	)
}

// MicroCompact 把除最近 KeepRecentToolResults 个之外的旧工具结果替换成占位提示，
// 返回一份新切片（不就地修改原消息结构）。防止上下文被旧结果持续霸占。
func MicroCompact(msgs []any) []any {
	// 收集所有工具结果消息的下标
	toolIdx := make([]int, 0, len(msgs))
	for i, m := range msgs {
		if _, ok := m.(client.ToolsMessage); ok {
			toolIdx = append(toolIdx, i)
		}
	}
	if len(toolIdx) <= KeepRecentToolResults {
		return msgs
	}
	// 需要压缩的是「最近 N 个之外」的更旧结果
	compactSet := make(map[int]struct{}, len(toolIdx)-KeepRecentToolResults)
	for _, idx := range toolIdx[:len(toolIdx)-KeepRecentToolResults] {
		compactSet[idx] = struct{}{}
	}

	out := make([]any, len(msgs))
	for i, m := range msgs {
		if _, hit := compactSet[i]; hit {
			tm := m.(client.ToolsMessage)
			if len(tm.Content) > microCompactMinLen && tm.Content != persistedPlaceholder {
				// 新建结构而非就地改字段：保留 ToolsId 以维持 tool_call 配对
				out[i] = client.ToolsMessage{
					Role:    tm.Role,
					Content: persistedPlaceholder,
					ToolsId: tm.ToolsId,
				}
				continue
			}
		}
		out[i] = m
	}
	return out
}

// EstimateContextSize 用消息序列化后的字节数粗略估算上下文大小。
func EstimateContextSize(msgs []any) int {
	b, err := json.Marshal(msgs)
	if err != nil {
		return 0
	}
	return len(b)
}

// SessionTranscript 把整个会话持续以 jsonl 落盘（每条消息一行），与压缩解耦：
// 无论是否触发压缩，用户在会话进行中随时都能拿到完整对话日志。
// 采用 append-only：只追加尚未写盘的新消息，已落盘的完整历史不会被压缩抹掉。
type SessionTranscript struct {
	path    string
	written int // 已落盘的消息条数（游标）
	mu      sync.Mutex
}

// NewSessionTranscript 创建会话级转录器，写入当前会话目录下的 session.jsonl。
func NewSessionTranscript() *SessionTranscript {
	return &SessionTranscript{path: filepath.Join(transcriptDir(), "session.jsonl")}
}

// Flush 把 msgs 中尚未落盘的消息追加写入。
// 压缩会把内存里的 msgs 收缩成更短的列表（如单条摘要），此时把收缩后的内容
// 视为新事件继续追加——已写盘的完整历史不受影响，磁盘日志单调增长。
func (t *SessionTranscript) Flush(msgs []any) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 列表变短说明发生了压缩：按新列表重新计数，把摘要等新内容继续追加
	if len(msgs) < t.written {
		t.written = 0
	}
	if t.written >= len(msgs) {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(t.path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(t.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	var sb strings.Builder
	for _, m := range msgs[t.written:] {
		line, err := json.Marshal(m)
		if err != nil {
			continue
		}
		sb.Write(line)
		sb.WriteByte('\n')
	}
	if _, err := f.WriteString(sb.String()); err != nil {
		return err
	}
	t.written = len(msgs)
	return nil
}

// Path 返回本会话转录文件的绝对路径。
func (t *SessionTranscript) Path() string { return t.path }

// summarizeHistory 调一次模型，把整段对话压成一份连续性摘要。
func summarizeHistory(ctx context.Context, call *client.Call, model string, msgs []any) (string, error) {
	raw, err := json.Marshal(msgs)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errors.ErrCompactSummary, err)
	}
	conversation := string(raw)
	if len(conversation) > summarizeInputLimit {
		conversation = conversation[:summarizeInputLimit]
	}
	summaryMsgs := []any{*call.Cm.NewUserMessage(prompt.CompactSummaryPromptPrefix + conversation)}
	res, resp, err := call.NewCallRequestCtx(ctx, model, summaryMsgs, false, prompt.CompactSummarySystemPrompt, nil, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errors.ErrCompactSummary, err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("%w: "+errors.ErrHTTPStatusCode, errors.ErrCompactSummary, resp.StatusCode)
	}
	if len(res.Choices) == 0 {
		return "", errors.ErrCompactSummary
	}
	if s, ok := res.Choices[0].Message.Content.(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s), nil
	}
	return "", errors.ErrCompactSummary
}

// CompactHistory 执行一次完整压缩：生成连续性摘要，
// 把整段历史替换成单条携带摘要的 user 消息。
// 摘要失败时返回原消息（不压缩），避免因瞬时错误抹掉上下文。
// 注意：完整对话已由 SessionTranscript 持续落盘，这里不再单独写转录。
func CompactHistory(ctx context.Context, call *client.Call, model string, msgs []any, state *CompactState, focus string) []any {
	summary, err := summarizeHistory(ctx, call, model, msgs)
	if err != nil {
		return msgs // 压缩失败：保住连续性，留待下一轮重试
	}

	if focus != "" {
		summary += "\n\nFocus to preserve next: " + focus
	}
	if files := recentReadFiles(); len(files) > 0 {
		var sb strings.Builder
		for _, f := range files {
			sb.WriteString("- " + f + "\n")
		}
		summary += "\n\nRecent files to reopen if needed:\n" + strings.TrimRight(sb.String(), "\n")
	}

	state.HasCompacted = true
	state.LastSummary = summary
	return []any{*call.Cm.NewUserMessage(prompt.CompactNotice + "\n\n" + summary)}
}

// recentReadFiles 从已读文件记录里取出最近碰过的文件（排序后截断），供摘要追踪。
func recentReadFiles() []string {
	ReadFiles.MU.RLock()
	files := make([]string, 0, len(ReadFiles.ReadFiles))
	for path := range ReadFiles.ReadFiles {
		files = append(files, path)
	}
	ReadFiles.MU.RUnlock()
	sort.Strings(files)
	if len(files) > recentFilesLimit {
		files = files[len(files)-recentFilesLimit:]
	}
	return files
}

// DetectManualCompact 检查本轮工具调用里是否有 compact，返回是否触发及其 focus 参数。
func DetectManualCompact(toolCalls []client.ToolCall) (bool, string) {
	for _, tc := range toolCalls {
		if tc.Function.Name != CompactToolName {
			continue
		}
		var args map[string]any
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		focus, _ := args["focus"].(string)
		return true, focus
	}
	return false, ""
}

// NewCompactTool 返回 compact 工具：模型主动请求压缩时调用。
// 真正的压缩在主循环里完成，这里只返回一个占位提示（与自动压缩复用同一条机制）。
func NewCompactTool() *Tools {
	return &Tools{
		Name: CompactToolName,
		Func: func(args map[string]any) string {
			return "Compacting conversation..."
		},
	}
}

func (t *Tools) CompactInfoForLLM() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        CompactToolName,
			Description: prompt.CompactPrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"focus": client.ParameterProperty{
						Type:        "string",
						Description: "Optional note on what must be preserved for the next steps.",
					},
				},
			},
		},
	}
}
