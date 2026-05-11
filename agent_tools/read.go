package agent_tools

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/tools"
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

func readFile(filePath string, offset int, limit int) (content string, totalLines int, startLine int, endLines int, isDirectory bool, truncated bool, isBinaryFile bool, err error) {
	// 1. 检查文件是否存在
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", 0, 0, 0, false, false, false, fmt.Errorf("%w: %w", errors.ErrFileNotExist, err)
		}
		return "", 0, 0, 0, false, false, false, fmt.Errorf("%w: %w", errors.ErrReadFile, err)
	}
	// 2. 检查文件是否为目录
	if info.IsDir() {
		return "", 0, 0, 0, true, false, false, nil
	}
	// 3.检查文件是否为二进制文件
	if isBinaryFile, ext, err := tools.IsBinaryFile(filePath); err != nil {
		return "", 0, 0, 0, false, false, false, fmt.Errorf("%w: %w", errors.ErrReadFile, fmt.Errorf("open file error:%w", err))
	} else if isBinaryFile {
		return "file is binary,ext:" + ext, 0, 0, 0, false, false, true, nil
	}
	// 4. 文件过大的情况
	// TODO: 文件过大的情况
	// 5. 读取文件内容
	f, err := os.Open(filePath)
	if err != nil {
		//TODO: 处理错误
	}
	defer f.Close()
	reader := bufio.NewReader(f)
	currentLine := 0
	lines := make([]string, 0, limit)
	for {
		lineBytes, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", 0, 0, 0, false, false, false, err
		}
		// 处理读到的内容（即使遇到 EOF，lineBytes 可能还有内容）
		if len(lineBytes) > 0 {
			currentLine++

			// 还没到 offset，跳过
			if currentLine < offset {
				continue
			}

			// 去掉 trailing \n 和 \r
			content := strings.TrimSuffix(string(lineBytes), "\n")
			content = strings.TrimSuffix(content, "\r")

			lines = append(lines, content)
			// 达到 limit，停止读取
			if len(lines) >= limit {
				break
			}
		}
	}
	var sb strings.Builder
	for i, line := range lines {
		sb.WriteString(fmt.Sprintf("%d | %s\n", offset+i, line))
	}
	return sb.String(), currentLine, offset, offset + len(lines) - 1, false, false, false, nil
}
