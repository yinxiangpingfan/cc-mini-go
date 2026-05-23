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
)
