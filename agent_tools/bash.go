package agent_tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

//bash工具

const DefaultTimeout = 120 * time.Second
const maxOutputChars = 10000

func bashTool(command string, description string, timeout time.Duration, dangerouslyDisableSandbox bool) (string, error) {
	//TODO:沙箱
	dangerouslyDisableSandbox = true
	//先默认禁用沙箱
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	// 构建命令
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Getenv("SHELL"), "-c", command)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	parts := []string{}
	if stdoutBuf.Len() > 0 {
		//去除\n\r
		stdoutStr := strings.TrimRight(stdoutBuf.String(), "\n\r")
		if count := utf8.RuneCountInString(stdoutStr); count > maxOutputChars {
			// 转为 rune 切片截取前 maxOutputChars 个字符
			runes := []rune(stdoutStr)
			stdoutStr = fmt.Sprintf("%s\n\n... (output truncated, full output was %d chars)", string(runes[:maxOutputChars]), count)
		}
		parts = append(parts, stdoutStr)
	}
	if stderrBuf.Len() > 0 {
		parts = append(parts, "[stderr]\n"+strings.TrimRight(stderrBuf.String(), "\n\r "))
	}
	//错误处理
	exitCode := 0
	if err != nil {
		// 检查是否是超时错误
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("Error: Command timed out after %s", timeout)
		}
		// 尝试获取退出码（非超时）
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			// 其他异常（如无法启动进程）
			return "", fmt.Errorf("Error: %v", err)
		}
	}
	if exitCode != 0 {
		parts = append(parts, fmt.Sprintf("[exit code: %d]", exitCode))
	}
	if len(parts) == 0 {
		parts = append(parts, "(no output)")
	}
	return strings.Join(parts, "\n"), nil
}

func NewBashTool() *Tools {
	return &Tools{
		Name: "Bash",
		Func: func(args map[string]interface{}) string {
			//从args中获取工具的参数
			command, exists := args["command"].(string)
			if !exists {
				return jsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "command"))
			}
			description, exists := args["description"].(string)
			if !exists {
				description = ""
			}
			timeout, exists := args["timeout"].(float64)
			if !exists {
				timeout = DefaultTimeout.Seconds()
			}
			if timeout <= 0 {
				timeout = DefaultTimeout.Seconds()
			}
			dangerouslyDisableSandbox, exists := args["dangerously_disable_sandbox"].(bool)
			if !exists {
				dangerouslyDisableSandbox = false
			}
			//执行工具
			output, err := bashTool(command, description, time.Duration(timeout)*time.Second, dangerouslyDisableSandbox)
			if err != nil {
				return jsonErr(err.Error())
			}
			b, _ := json.Marshal(map[string]string{"output": output})
			return string(b)
		},
	}
}

func (t *Tools) BashToolForLLM() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "Bash",
			Description: prompt.BashPrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]client.ParameterProperty{
					"command": {
						Type:        "string",
						Description: "The bash command to execute",
					},
					"description": {
						Type:        "string",
						Description: "Clear, concise description of what this command does in active voice",
					},
					"timeout": {
						Type:        "integer",
						Description: "Timeout in seconds",
					},
					"dangerously_disable_sandbox": {
						Type:        "boolean",
						Description: "If true and allowed by config, run outside sandbox",
					},
				},
				Required: []string{"command"},
			},
		},
	}
}
