package errors

import "fmt"

var (
	// ErrToolFunctionCall 工具调用参数缺失或非法，使用时 fmt.Sprintf(ErrToolFunctionCall, argName)
	ErrToolFunctionCall = "tool argument '%s' is missing or invalid"
	// ErrFileNotExist 文件不存在
	ErrFileNotExist = fmt.Errorf("file does not exist")
	// ErrReadFile 文件读取失败
	ErrReadFile = fmt.Errorf("read file failed")
	// ErrFileTooLarge 文件过大
	ErrFileTooLarge = fmt.Errorf("file too large")
	// ErrMarshalResponse 序列化响应失败
	ErrMarshalResponse = fmt.Errorf("failed to serialize response")
	// ErrHashFile 计算文件hash失败
	ErrHashFile = fmt.Errorf("failed to compute file hash")
	// ErrWriteFile 写入文件失败
	ErrWriteFile = fmt.Errorf("write file failed")
	// ErrFileNotRead 文件未读取就尝试覆盖
	ErrFileNotRead = fmt.Errorf("file must be read before overwriting, please read the file again")
	// ErrFileModified 文件在读取后被修改
	ErrFileModified = fmt.Errorf("file has been modified since last read, please read the file again")
	// ErrBashTimeout bash 命令执行超时，使用时 fmt.Errorf(ErrBashTimeout, timeout)
	ErrBashTimeout = "command timed out after %s"
	// ErrBashExec bash 命令无法启动或执行失败
	ErrBashExec = fmt.Errorf("command execution failed")
	// ErrInvalidTimezone 时区加载失败
	ErrInvalidTimezone = fmt.Errorf("invalid timezone")
	// ErrTodoInProgress 同一时间只能有一个进行中的任务
	ErrTodoInProgress = fmt.Errorf("only one item can be in_progress at a time")
	// ErrSubAgentRequest 子 agent 请求失败
	ErrSubAgentRequest = fmt.Errorf("subagent request failed")
	// ErrUnknownSkill 加载了不存在的 skill，使用时 fmt.Sprintf(ErrUnknownSkill, name, knownList)
	ErrUnknownSkill = "unknown skill '%s'. available skills: %s"
	// ErrCompactSummary 压缩时生成对话摘要失败
	ErrCompactSummary = fmt.Errorf("failed to summarize conversation for compaction")
	// ErrPathIsDirectory 目标是目录而非文件
	ErrPathIsDirectory = fmt.Errorf("path is a directory, not a file")
	// ErrEditOldStringNotFound edit 的 old_string 在文件中找不到
	ErrEditOldStringNotFound = fmt.Errorf("old_string not found in file")
	// ErrEditOldStringNotUnique old_string 出现多次但未开启 replace_all，使用时 fmt.Errorf(ErrEditOldStringNotUnique, count)
	ErrEditOldStringNotUnique = "old_string is not unique (found %d times); add more surrounding context to make it unique, or set replace_all=true"
	// ErrEditNoChange old_string 与 new_string 相同，无可替换
	ErrEditNoChange = fmt.Errorf("old_string and new_string are identical, nothing to replace")
	// ErrInvalidRegex grep 正则表达式非法
	ErrInvalidRegex = fmt.Errorf("invalid regular expression")
	// ErrInvalidGlobPattern glob 模式非法
	ErrInvalidGlobPattern = fmt.Errorf("invalid glob pattern")
	// ErrSearchPath 搜索路径无法访问
	ErrSearchPath = fmt.Errorf("cannot access search path")
)
