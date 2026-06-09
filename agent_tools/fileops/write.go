package fileops

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	filePath "path/filepath"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
	"github.com/yinxiangpingfan/cc-mini-go/tools"
)

// writeFile writes data to a file named by filename.
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
		var err error
		fileHash, err = tools.HashFile(file_Path)
		if err != nil {
			return fmt.Errorf("%w: %w", errors.ErrHashFile, err)
		}
		shared.ReadFileState.MU.RLock()
		fileHashed, ok := shared.ReadFileState.ReadFiles[file_Path]
		shared.ReadFileState.MU.RUnlock()
		if !ok {
			return fmt.Errorf("%w: %s", errors.ErrFileNotRead, file_Path)
		}
		if fileHashed != fileHash {
			return fmt.Errorf("%w: %s", errors.ErrFileModified, file_Path)
		}
		// hash 一致，允许覆盖写入
		if err := os.WriteFile(file_Path, []byte(content), 0644); err != nil {
			return fmt.Errorf("%w: %w", errors.ErrWriteFile, err)
		}
	}
	//更新文件哈希
	fileHash, err := tools.HashFile(file_Path)
	if err != nil {
		return fmt.Errorf("%w: %w", errors.ErrHashFile, err)
	}
	shared.ReadFileState.MU.Lock()
	shared.ReadFileState.ReadFiles[file_Path] = fileHash
	shared.ReadFileState.MU.Unlock()
	return nil
}

func NewWriteFileTool() *Tools {
	return &Tools{
		Name: "write_file",
		Func: func(ctx context.Context, args map[string]interface{}) string {
			//从args中获取工具的参数
			filePath, exists := args["file_path"].(string)
			if !exists {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "file_path"))
			}
			content, exists := args["content"].(string)
			if !exists {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "content"))
			}
			err := writeFile(filePath, content)
			if err != nil {
				return shared.JsonErr(err.Error())
			}
			lines := strings.Count(content, "\n") + 1
			b, _ := json.Marshal(map[string]interface{}{"success": true, "lines": lines, "path": filePath})
			return string(b)
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
				Properties: map[string]any{
					"file_path": client.ParameterProperty{
						Description: "Absolute path to the file to write",
						Type:        "string",
					},
					"content": client.ParameterProperty{
						Description: "The full content to write to the file",
						Type:        "string",
					},
				},
				Required: []string{"file_path", "content"},
			},
		},
	}
}
