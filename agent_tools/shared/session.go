package shared

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SessionStorageDir 是本次会话的存储根，进程启动时确定一次，整个会话共用。
// 布局：~/.cc_mini_go/projects/<项目>/<会话>/ —— 按项目、按会话分别隔离。
// 测试可覆盖该变量以隔离。
var SessionStorageDir = DefaultSessionStorageDir()

// ToolResultsDir 大工具结果落盘目录（会话级）
func ToolResultsDir() string { return filepath.Join(SessionStorageDir, "tool-results") }

// TranscriptDir 完整对话转录目录（会话级）
func TranscriptDir() string { return filepath.Join(SessionStorageDir, "transcripts") }

// TaskDir 持久化任务图目录（会话级，s12）
func TaskDir() string { return filepath.Join(SessionStorageDir, "tasks") }

// DefaultSessionStorageDir 计算 ~/.cc_mini_go/projects/<项目>/<会话> 绝对路径。
// 取不到 home 时退回当前目录下的 .cc_mini_go，保证始终可写。
func DefaultSessionStorageDir() string {
	base := ".cc_mini_go"
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		base = filepath.Join(home, ".cc_mini_go")
	}
	return filepath.Join(base, "projects", ProjectSlug(), NewSessionID())
}

// ProjectSlug 把当前工作目录的绝对路径转成文件名安全的项目标识。
func ProjectSlug() string {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		return "unknown"
	}
	slug := strings.ReplaceAll(cwd, string(os.PathSeparator), "-")
	slug = strings.ReplaceAll(slug, ":", "-")
	if slug == "" {
		return "unknown"
	}
	return slug
}

// NewSessionID 生成本次会话的唯一标识（时间戳 + 纳秒，便于排序与去重）。
func NewSessionID() string {
	now := time.Now()
	return fmt.Sprintf("%s-%09d", now.Format("20060102-150405"), now.UnixNano()%1e9)
}
