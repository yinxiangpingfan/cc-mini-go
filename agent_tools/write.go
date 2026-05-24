package agent_tools

import (
	"fmt"
	"os"
	"strings"

	filePath "path/filepath"

	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
	"github.com/yinxiangpingfan/cc-mini-go/tools"
)

// WriteFile writes data to a file named by filename.
func writeFile(file_Path string, content string) error {
	var fileHash string
	//检查文件是否存在
	if _, err := os.Stat(file_Path); err != nil {
		// 文件不存在，可以创建
		//创建路径
		if err := os.MkdirAll(filePath.Dir(file_Path), 0755); err != nil {
			return fmt.Errorf("%w: create directory: %w", errors.ErrWriteFile, err)
		}
		if err := os.WriteFile(file_Path, []byte(content), 0644); err != nil {
			return fmt.Errorf("%w: %w", errors.ErrWriteFile, err)
		}
	} else {
		// 文件存在
		//检查文件是否读过
		fileHash, err = tools.HashFile(file_Path)
		if err != nil {
			return fmt.Errorf("%w: %w", errors.ErrHashFile, err)
		}
		if fileHashed, ok := ReadFiles[file_Path]; !ok {
			return fmt.Errorf("%w: %s", errors.ErrFileNotRead, file_Path)
		} else {
			if fileHashed != fileHash {
				return fmt.Errorf("%w: %s", errors.ErrFileModified, file_Path)
			}
			// hash 一致，允许覆盖写入
			if err := os.WriteFile(file_Path, []byte(content), 0644); err != nil {
				return fmt.Errorf("%w: %w", errors.ErrWriteFile, err)
			}
		}
	}
	//更新文件哈希
	fileHash, err := tools.HashFile(file_Path)
	if err != nil {
		return fmt.Errorf("%w: %w", errors.ErrHashFile, err)
	}
	ReadFiles[file_Path] = fileHash
	return nil
}

func NewWriteFileTool() *Tools {
	return &Tools{
		Name: "write_file",
		Func: func(args map[string]interface{}) string {
			//从args中获取工具的参数
			filePath, exists := args["file_path"].(string)
			if !exists {
				return fmt.Sprintf("{\"error\": \"%s\"}", fmt.Sprintf(errors.ErrToolFunctionCall, "file_path"))
			}
			content, exists := args["content"].(string)
			if !exists {
				return fmt.Sprintf("{\"error\": \"%s\"}", fmt.Sprintf(errors.ErrToolFunctionCall, "content"))
			}
			err := writeFile(filePath, content)
			if err != nil {
				return fmt.Sprintf("{\"error\": \"%s\"}", err.Error())
			}
			lines := strings.Count(content, "\n") + 1
			return fmt.Sprintf("{\"success\": true, \"lines\": %d, \"path\": \"%s\"}", lines, filePath)
		},
	}
}

func (t *Tools) WriteFileInfoForLLm() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "write_file",
			Description: prompt.WriteFilePrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]client.ParameterProperty{
					"file_path": {
						Description: "Absolute path to the file to write",
						Type:        "string",
					},
					"content": {
						Description: "The full content to write to the file",
						Type:        "string",
					},
				},
				Required: []string{"file_path", "content"},
			},
		},
	}
}
