package log

import (
	"fmt"
	"os"

	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// openWriteSyncer 根据 output 字符串返回 WriteSyncer
// - "stdout"/"" → os.Stdout
// - "stderr"    → os.Stderr
// - 其它路径   → lumberjack 滚动日志(size/age/backups 由 rot 控制)
func openWriteSyncer(output string, rot Rotation) (zapcore.WriteSyncer, error) {
	switch output {
	case "stdout", "":
		return zapcore.AddSync(os.Stdout), nil
	case "stderr":
		return zapcore.AddSync(os.Stderr), nil
	default:
		// 确保父目录存在(常见场景 ./data/agent.log)
		if dir := dirname(output); dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("mkdir log dir %q: %w", dir, err)
			}
		}
		// Rotation 全 0 视作"不切",退化为单文件 OpenFile 行为
		if rot.MaxSizeMB == 0 && rot.MaxAgeDays == 0 && rot.MaxBackups == 0 {
			f, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				return nil, fmt.Errorf("open log file %q: %w", output, err)
			}
			return zapcore.AddSync(f), nil
		}
		lj := &lumberjack.Logger{
			Filename:   output,
			MaxSize:    rot.MaxSizeMB, // MB,<=0 用 lumberjack 默认 100
			MaxAge:     rot.MaxAgeDays, // days,<=0 不删
			MaxBackups: rot.MaxBackups, // 保留数,<=0 全留
			Compress:   rot.Compress,   // gzip 老文件
			LocalTime:  true,           // 用本地时间作为 rotated 文件名后缀
		}
		return zapcore.AddSync(lj), nil
	}
}

// dirname 返回 path 的目录(空字符串表示无目录组件)
func dirname(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return ""
}
