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
)
