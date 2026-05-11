package agent_tools

import (
	"fmt"
	"os"

	"github.com/yinxiangpingfan/cc-mini-go/errors"
)

type request struct {
	FilePath string `json:"file_path"` // 文件或目录
	Offset   int    `json:"offset"`    // 起始行（1-based，默认 1）
	Limit    int    `json:"limit"`     // 最大行数（默认 50，最大 200）
}
type response struct {
	Content      string `json:"content"`        // 带行号的内容 或 目录列表
	TotalLines   int    `json:"total_lines"`    // 文件总行数（如果是文件）
	StartLine    int    `json:"start_line"`     // 本次起始行
	EndLine      int    `json:"end_line"`       // 本次结束行
	IsDirectory  bool   `json:"is_directory"`   // 是否是目录
	Truncated    bool   `json:"truncated"`      // 是否被截断
	IsBinaryFile bool   `json:"is_binary_file"` // 是否是二进制文件
}

func readFile(FilePath string, Offset int, Limit int) (content string, totalLines int, startLine int, endLines int, isDirectory bool, truncated bool, isBinaryFile bool, err error) {
	// 1. 检查文件是否存在
	if _, err := os.Stat(FilePath); os.IsNotExist(err) {
		return "", 0, 0, 0, false, false, false, fmt.Errorf("%w: %w", errors.ErrFileNotExist, err)
	} else {
		return "", 0, 0, 0, false, false, false, fmt.Errorf("%w: %w", errors.ErrReadFile, err)
	}
}
