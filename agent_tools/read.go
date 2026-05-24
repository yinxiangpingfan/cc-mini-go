package agent_tools

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
	"github.com/yinxiangpingfan/cc-mini-go/tools"
)

type response struct {
	Content      string `json:"content"`        // 带行号的内容 或 目录列表
	TotalLines   int    `json:"total_lines"`    // 文件总行数（如果是文件）
	StartLine    int    `json:"start_line"`     // 本次起始行
	EndLine      int    `json:"end_line"`       // 本次结束行
	IsDirectory  bool   `json:"is_directory"`   // 是否是目录
	Truncated    bool   `json:"truncated"`      // 是否被截断
	IsBinaryFile bool   `json:"is_binary_file"` // 是否是二进制文件
}

// maxFileSize 允许读取的最大文件大小（1gb）
const maxFileSize = 1 * 1024 * 1024 * 1024

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
	// 3. 检查文件是否为二进制文件
	if isBinaryFile, ext, err := tools.IsBinaryFile(filePath); err != nil {
		return "", 0, 0, 0, false, false, false, fmt.Errorf("%w: %w", errors.ErrReadFile, fmt.Errorf("check if the file is a binary file error:%w", err))
	} else if isBinaryFile {
		return "file is binary,ext:" + ext, 0, 0, 0, false, false, true, nil
	}
	// 4. 检查文件是否过大
	if info.Size() > maxFileSize {
		return "", 0, 0, 0, false, false, false, fmt.Errorf("%w: size %d bytes exceeds limit %d bytes", errors.ErrFileTooLarge, info.Size(), maxFileSize)
	}
	// 5. 读取文件内容
	f, err := os.Open(filePath)
	if err != nil {
		return "", 0, 0, 0, false, false, false, fmt.Errorf("%w: %w", errors.ErrReadFile, err)
	}
	defer f.Close()
	reader := bufio.NewReader(f)
	currentLine := 0
	lines := make([]string, 0, limit)
	hitLimit := false
	for {
		lineBytes, err := reader.ReadBytes('\n')
		// 处理读到的内容（即使遇到 EOF，lineBytes 可能还有内容）
		if len(lineBytes) > 0 {
			currentLine++
			// 还没到 offset，跳过
			if currentLine < offset {
				goto checkErr
			}
			// 已达到 limit，不再收集内容，但继续计数总行数
			if hitLimit {
				goto checkErr
			}
			// 去掉 trailing \n 和 \r
			line := strings.TrimSuffix(string(lineBytes), "\n")
			line = strings.TrimSuffix(line, "\r")
			lines = append(lines, line)
			// 达到 limit，标记截断
			if len(lines) >= limit {
				hitLimit = true
			}
		}
	checkErr:
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", 0, 0, 0, false, false, false, fmt.Errorf("%w: %w", errors.ErrReadFile, fmt.Errorf("read file error:%w", err))
		}
	}
	var sb strings.Builder
	for i, line := range lines {
		sb.WriteString(fmt.Sprintf("%d | %s\n", offset+i, line))
	}
	return sb.String(), currentLine, offset, offset + len(lines) - 1, false, hitLimit, false, nil
}

func NewReadFile() *Tools {
	return &Tools{
		Name: "read_file",
		Func: func(args map[string]interface{}) string {
			//从args中获取工具的参数
			filePath, exists := args["file_path"].(string)
			if !exists {
				return fmt.Sprintf("{\"error\": \"%s\"}", fmt.Sprintf(errors.ErrToolFunctionCall, "file_path"))
			}
			offset, exists := args["offset"].(int)
			if !exists {
				offset = 1
			}
			if offset < 1 {
				offset = 1
			}
			limit, exists := args["limit"].(int)
			if !exists {
				limit = 2000
			}
			if limit < 1 {
				limit = 2000
			}
			res := response{}
			var err error
			res.Content, res.TotalLines, res.StartLine, res.EndLine, res.IsDirectory, res.Truncated, res.IsBinaryFile, err = readFile(filePath, offset, limit)
			if err != nil {
				return fmt.Sprintf("{\"error\": \"%s\"}", err.Error())
			}
			jsonBytes, err := json.Marshal(res)
			if err != nil {
				return fmt.Sprintf("{\"error\": \"%s\"}", fmt.Errorf("%w: %w", errors.ErrMarshalResponse, err))
			}
			//把文件标为已读
			hash, err := tools.HashFile(filePath)
			if err != nil {
				return fmt.Sprintf("{\"error\": \"%s\"}", fmt.Errorf("%w: %w", errors.ErrHashFile, err))
			}
			ReadFiles[filePath] = hash
			return string(jsonBytes)
		},
	}
}

func (t *Tools) ReadFileInfoForLLm() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "read_file",
			Description: prompt.ReadFilePrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]client.ParameterProperty{
					"file_path": {
						Type:        "string",
						Description: "Absolute path to the file",
					},
					"offset": {
						Type:        "integer",
						Description: "Line to start from (1-indexed) default 1",
					},
					"limit": {
						Type:        "integer",
						Description: "Max lines to return (default 2000)",
					},
				},
				Required: []string{"file_path"},
			},
		},
	}
}
