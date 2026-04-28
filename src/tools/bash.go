package tools

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// Default command timeout in seconds.
// 默认命令超时时间（秒）。
const defaultTimeout = 120

// BashRequest is the input for the bash command tool.
// BashRequest 是执行 bash 命令工具的输入参数。
type BashRequest struct {
	Command     string `json:"command" jsonschema:"required,description=The bash command to execute"`
	Description string `json:"description" jsonschema:"description=Clear, concise description of what this command does"`
	Timeout     int    `json:"timeout" jsonschema:"description=Timeout in seconds, default 120"`
}

// BashResult is the output of the bash command execution.
// BashResult 是 bash 命令执行后的输出结果。
type BashResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// bashExec executes a bash command and returns the result.
// bashExec 执行 bash 命令并返回结果。
func bashExec(ctx context.Context, req *BashRequest) (*BashResult, error) {
	command := req.Command
	timeout := req.Timeout

	// Use default timeout if not specified or invalid.
	// 如果未指定或无效，则使用默认超时时间。
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	// Execute command via bash -c to support shell features.
	// 通过 bash -c 执行命令，以支持 shell 特性。
	cmd := exec.Command("bash", "-c", command)

	// Capture stdout and stderr separately.
	// 分别捕获标准输出和标准错误。
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set up context with timeout.
	// 设置带超时的上下文。
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// Run the command.
	// 执行命令。
	err := cmd.Run()

	// Check if command timed out.
	// 检查命令是否超时。
	select {
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			// Kill the process if it's still running.
			// 如果进程仍在运行，则终止它。
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
			return &BashResult{
				Content: "Error: Command timed out after " + strconv.Itoa(timeout) + "s",
				IsError: true,
			}, nil
		}
	default:
	}

	// Get output strings.
	// 获取输出字符串。
	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	var parts []string

	// Process stdout, truncate if too long (>10KB).
	// 处理标准输出，如果过长则截断（>10KB）。
	if stdoutStr != "" {
		out := stdoutStr
		if len(out) > 10000 {
			out = out[:10000] + "\n\n... (output truncated, full output was " + strconv.Itoa(len(stdoutStr)) + " chars)"
		}
		parts = append(parts, strings.TrimRight(out, "\n"))
	}

	// Process stderr with marker.
	// 处理标准错误，添加标记。
	if stderrStr != "" {
		parts = append(parts, "[stderr]\n"+strings.TrimRight(stderrStr, "\n"))
	}

	// Append exit code if command failed.
	// 如果命令失败，追加退出码。
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			parts = append(parts, "[exit code: "+strconv.Itoa(exitErr.ExitCode())+"]")
		} else {
			parts = append(parts, "[exit code: unknown]")
		}
	}

	// Join all parts with newline.
	// 用换行符连接所有部分。
	content := strings.Join(parts, "\n")
	if content == "" {
		content = "(no output)"
	}

	return &BashResult{Content: content, IsError: false}, nil
}

// NewBashTool creates an InvokableTool for executing bash commands.
// NewBashTool 创建一个可调用的 bash 命令执行工具。
func NewBashTool() (tool.InvokableTool, error) {
	return utils.InferTool("Bash",
		"Executes a given bash command and returns its output.\n\n"+
			"The working directory persists between commands, but shell state does not. "+
			"The shell environment is initialized from the user's profile (bash or zsh).\n\n"+
			"IMPORTANT: Avoid using this tool to run `find`, `grep`, `cat`, `head`, `tail`, "+
			"`sed`, `awk`, or `echo` commands, unless explicitly instructed:\n\n"+
			" - File search: Use Glob\n"+
			" - Content search: Use Grep\n"+
			" - Read files: Use Read\n"+
			" - Edit files: Use Edit\n"+
			" - Write files: Use Write\n\n"+
			"Usage:\n"+
			"- If your command will create new directories or files, first use `ls` to verify the parent directory exists.\n"+
			"- Always quote file paths that contain spaces with double quotes.\n"+
			"- Use absolute paths to avoid changing working directory.\n"+
			"- For multiple commands: use `&&` for sequential dependency, `;` for independent.\n"+
			"- Git commands: prefer new commits over amending, avoid force push.\n"+
			"- Avoid unnecessary `sleep` commands.",
		bashExec)
}
