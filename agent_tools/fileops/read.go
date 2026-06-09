package fileops

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
	"github.com/yinxiangpingfan/cc-mini-go/tools"
)

// imageExtensions 支持以多模态方式读取的图片扩展名（光栅图）。
var imageExtensions = map[string]bool{
	"png": true, "jpg": true, "jpeg": true, "gif": true, "webp": true, "bmp": true,
}

// imageExt 若 path 是支持的图片，返回小写扩展名（不含点）与 true。
func imageExt(path string) (string, bool) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if imageExtensions[ext] {
		return ext, true
	}
	return "", false
}

// imageToolResult 是图片类工具结果的载体：Content 给模型看的文字摘要，ImageURL 是 data URI。
// 工具结果本身是字符串（ToolsMessage），图片真正以多模态 user 消息进上下文（见 SplitImageResult）。
type imageToolResult struct {
	Image    bool   `json:"__image__"`
	Content  string `json:"content"`
	ImageURL string `json:"image_url"`
}

// newImageResult 把图片摘要 + data URI 打包成工具结果字符串。
func newImageResult(content, dataURI string) string {
	b, _ := json.Marshal(imageToolResult{Image: true, Content: content, ImageURL: dataURI})
	return string(b)
}

// SplitImageResult 解析工具结果：若是图片，返回(文字摘要, dataURI, true)；否则原样返回(res, "", false)。
// agent 循环据此把图片 data URI 拆出来，作为独立的多模态 user 消息接在工具结果之后。
func SplitImageResult(res string) (content, imageURI string, isImage bool) {
	if !strings.Contains(res, `"__image__"`) {
		return res, "", false
	}
	var r imageToolResult
	if err := json.Unmarshal([]byte(res), &r); err != nil || !r.Image {
		return res, "", false
	}
	return r.Content, r.ImageURL, true
}

// readImageFile 把图片读成 base64 data URI，封装成图片工具结果。
func readImageFile(filePath, ext string) string {
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return shared.JsonErr(fmt.Errorf("%w: %w", errors.ErrFileNotExist, err).Error())
		}
		return shared.JsonErr(fmt.Errorf("%w: %w", errors.ErrReadFile, err).Error())
	}
	if info.IsDir() {
		return shared.JsonErr(fmt.Sprintf("%s is a directory, not a file", filePath))
	}
	if info.Size() > maxFileSize {
		return shared.JsonErr(fmt.Errorf("%w: size %d bytes exceeds limit %d bytes", errors.ErrFileTooLarge, info.Size(), maxFileSize).Error())
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return shared.JsonErr(fmt.Errorf("%w: %w", errors.ErrReadFile, err).Error())
	}
	mediaType := "image/" + ext
	if ext == "jpg" {
		mediaType = "image/jpeg"
	}
	dataURI := "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
	content := fmt.Sprintf("[Image: %s (%s, %d bytes)]", filePath, mediaType, info.Size())
	return newImageResult(content, dataURI)
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
	return sb.String(), currentLine, offset, offset+len(lines)-1, false, hitLimit, false, nil
}

// Tools defines the tool structure locally to allow package-local method definitions.
type Tools struct {
	Name string
	Func shared.ToolFunc `json:"-"`
}

func NewReadFile() *Tools {
	return &Tools{
		Name: "read_file",
		Func: func(ctx context.Context, args map[string]interface{}) string {
			//从args中获取工具的参数
			filePath, exists := args["file_path"].(string)
			if !exists {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "file_path"))
			}
			// 图片：读成 data URI，由 agent 循环转成多模态 user 消息让模型真正“看”到
			if ext, ok := imageExt(filePath); ok {
				return readImageFile(filePath, ext)
			}
			// JSON 数字经 map[string]any 解码后是 float64，不能直接断言成 int，
			// 否则 LLM 传的 offset/limit 会被忽略、永远用默认值。
			offset := 1
			if v, ok := shared.AsInt(args["offset"]); ok && v >= 1 {
				offset = v
			}
			limit := 2000
			if v, ok := shared.AsInt(args["limit"]); ok && v >= 1 {
				limit = v
			}
			res := response{}
			var err error
			res.Content, res.TotalLines, res.StartLine, res.EndLine, res.IsDirectory, res.Truncated, res.IsBinaryFile, err = readFile(filePath, offset, limit)
			if err != nil {
				return shared.JsonErr(err.Error())
			}
			jsonBytes, err := json.Marshal(res)
			if err != nil {
				return shared.JsonErr(fmt.Errorf("%w: %w", errors.ErrMarshalResponse, err).Error())
			}
			//把文件标为已读
			hash, err := tools.HashFile(filePath)
			if err != nil {
				return shared.JsonErr(fmt.Errorf("%w: %w", errors.ErrHashFile, err).Error())
			}
			shared.ReadFileState.MU.Lock()
			shared.ReadFileState.ReadFiles[filePath] = hash
			shared.ReadFileState.MU.Unlock()
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
				Properties: map[string]any{
					"file_path": client.ParameterProperty{
						Type:        "string",
						Description: "Absolute path to the file",
					},
					"offset": client.ParameterProperty{
						Type:        "integer",
						Description: "Line to start from (1-indexed) default 1",
					},
					"limit": client.ParameterProperty{
						Type:        "integer",
						Description: "Max lines to return (default 2000)",
					},
				},
				Required: []string{"file_path"},
			},
		},
	}
}
