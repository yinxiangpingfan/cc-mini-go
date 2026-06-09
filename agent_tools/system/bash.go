package system

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

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

const DefaultTimeout = 120 * time.Second
const maxOutputChars = 10000

func bashTool(ctx context.Context, command string, description string, timeout time.Duration, dangerouslyDisableSandbox bool) (string, error) {
	dangerouslyDisableSandbox = true
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	// 构建命令：派生自传入 ctx，父 ctx 取消会立刻终止命令（超时与取消二者先到先生效）
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, os.Getenv("SHELL"), "-c", command)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	parts := []string{}
	if stdoutBuf.Len() > 0 {
		stdoutStr := strings.TrimRight(stdoutBuf.String(), "\n\r")
		if count := utf8.RuneCountInString(stdoutStr); count > maxOutputChars {
			runes := []rune(stdoutStr)
			stdoutStr = fmt.Sprintf("%s\n\n... (output truncated, full output was %d chars)", string(runes[:maxOutputChars]), count)
		}
		parts = append(parts, stdoutStr)
	}
	if stderrBuf.Len() > 0 {
		parts = append(parts, "[stderr]\n"+strings.TrimRight(stderrBuf.String(), "\n\r "))
	}
	exitCode := 0
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if cmdCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf(errors.ErrBashTimeout, timeout)
		}
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			return "", fmt.Errorf("%w: %w", errors.ErrBashExec, err)
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

// Tools defines the tool structure locally to allow package-local method definitions.
type Tools struct {
	Name string
	Func shared.ToolFunc `json:"-"`
}

func NewBashTool() *Tools {
	return &Tools{
		Name: "Bash",
		Func: func(ctx context.Context, args map[string]interface{}) string {
			command, exists := args["command"].(string)
			if !exists {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "command"))
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
			output, err := bashTool(ctx, command, description, time.Duration(timeout*float64(time.Second)), dangerouslyDisableSandbox)
			if err != nil {
				return shared.JsonErr(err.Error())
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
				Properties: map[string]any{
					"command": client.ParameterProperty{
						Type:        "string",
						Description: "The bash command to execute",
					},
					"description": client.ParameterProperty{
						Type:        "string",
						Description: "Clear, concise description of what this command does in active voice",
					},
					"timeout": client.ParameterProperty{
						Type:        "integer",
						Description: "Timeout in seconds",
					},
					"dangerously_disable_sandbox": client.ParameterProperty{
						Type:        "boolean",
						Description: "If true and allowed by config, run outside sandbox",
					},
				},
				Required: []string{"command"},
			},
		},
	}
}
