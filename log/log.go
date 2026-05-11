package log

import (
	"log/slog"
	"os"
)

func InitLogger() *slog.Logger {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelDebug, // 最低级别
		AddSource: true,            // 显示文件名和行号
	}))
	return logger
}
