package agent_tools

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
	"github.com/yinxiangpingfan/cc-mini-go/tools"
)

// editFile 对已存在文件做精确字符串替换，返回替换次数。
// 与 write_file 一致地强制「先读后写」：文件必须先被 read_file 读取过，
// 且自读取以来未在磁盘上被外部修改（哈希一致），否则拒绝编辑。
func editFile(filePath string, oldString string, newString string, replaceAll bool) (int, error) {
	// 1. 文件存在性与类型检查
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("%w: %s", errors.ErrFileNotExist, filePath)
		}
		return 0, fmt.Errorf("%w: %w", errors.ErrReadFile, err)
	}
	if info.IsDir() {
		return 0, fmt.Errorf("%w: %s", errors.ErrPathIsDirectory, filePath)
	}
	// 2. 文件过大
	if info.Size() > maxFileSize {
		return 0, fmt.Errorf("%w: size %d bytes exceeds limit %d bytes", errors.ErrFileTooLarge, info.Size(), maxFileSize)
	}

	// 3. 先读后写校验（与 write.go 同一约束）
	curHash, err := tools.HashFile(filePath)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errors.ErrHashFile, err)
	}
	ReadFiles.MU.RLock()
	recordedHash, ok := ReadFiles.ReadFiles[filePath]
	ReadFiles.MU.RUnlock()
	if !ok {
		return 0, fmt.Errorf("%w: %s", errors.ErrFileNotRead, filePath)
	}
	if recordedHash != curHash {
		return 0, fmt.Errorf("%w: %s", errors.ErrFileModified, filePath)
	}

	// 4. 读取内容
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errors.ErrReadFile, err)
	}
	content := string(data)

	// 5. 计数并校验唯一性
	count := strings.Count(content, oldString)
	if count == 0 {
		return 0, fmt.Errorf("%w: %s", errors.ErrEditOldStringNotFound, filePath)
	}
	if count > 1 && !replaceAll {
		return 0, fmt.Errorf(errors.ErrEditOldStringNotUnique, count)
	}

	// 6. 替换
	var newContent string
	if replaceAll {
		newContent = strings.ReplaceAll(content, oldString, newString)
	} else {
		newContent = strings.Replace(content, oldString, newString, 1)
	}

	// 7. 写回（保留原文件权限）
	if err := os.WriteFile(filePath, []byte(newContent), info.Mode().Perm()); err != nil {
		return 0, fmt.Errorf("%w: %w", errors.ErrWriteFile, err)
	}

	// 8. 更新哈希记录，以便后续可继续 edit/write
	newHash, err := tools.HashFile(filePath)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errors.ErrHashFile, err)
	}
	ReadFiles.MU.Lock()
	ReadFiles.ReadFiles[filePath] = newHash
	ReadFiles.MU.Unlock()

	if replaceAll {
		return count, nil
	}
	return 1, nil
}

func NewEditFileTool() *Tools {
	return &Tools{
		Name: "edit_file",
		Func: func(args map[string]interface{}) string {
			filePath, ok := args["file_path"].(string)
			if !ok || filePath == "" {
				return jsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "file_path"))
			}
			// old_string 为空会匹配到任意位置，视为非法参数
			oldString, ok := args["old_string"].(string)
			if !ok || oldString == "" {
				return jsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "old_string"))
			}
			// new_string 允许为空（用于删除文本），仅要求类型正确
			newString, ok := args["new_string"].(string)
			if !ok {
				return jsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "new_string"))
			}
			if oldString == newString {
				return jsonErr(errors.ErrEditNoChange.Error())
			}
			// replace_all 可选，默认 false
			replaceAll, _ := args["replace_all"].(bool)

			replaced, err := editFile(filePath, oldString, newString, replaceAll)
			if err != nil {
				return jsonErr(err.Error())
			}
			b, _ := json.Marshal(map[string]any{"success": true, "replaced": replaced, "path": filePath})
			return string(b)
		},
	}
}

func (t *Tools) EditFileInfoForLLm() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "edit_file",
			Description: prompt.EditFilePrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"file_path": client.ParameterProperty{
						Type:        "string",
						Description: "Absolute path to the file to edit",
					},
					"old_string": client.ParameterProperty{
						Type:        "string",
						Description: "The exact text to replace (must be unique unless replace_all is set)",
					},
					"new_string": client.ParameterProperty{
						Type:        "string",
						Description: "The replacement text (must differ from old_string)",
					},
					"replace_all": client.ParameterProperty{
						Type:        "boolean",
						Description: "Replace all occurrences of old_string (default false)",
					},
				},
				Required: []string{"file_path", "old_string", "new_string"},
			},
		},
	}
}
