// Package log 封装 zap 日志器
// 集中处理:level / format / output / rotation
package log

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Config 日志配置(对应 config/agent.yaml 的 log 节点)
type Config struct {
	Level    string     `mapstructure:"level"`
	Format   string     `mapstructure:"format"`
	Output   string     `mapstructure:"output"`
	NoColor  bool       `mapstructure:"no_color"`
	Rotation Rotation   `mapstructure:"rotation"`
}

// Rotation 日志切割(后续可接入 lumberjack,W1 先支持基础 stdout)
type Rotation struct {
	MaxSizeMB  int  `mapstructure:"max_size_mb"`
	MaxAgeDays int  `mapstructure:"max_age_days"`
	MaxBackups int  `mapstructure:"max_backups"`
	Compress   bool `mapstructure:"compress"`
}

// New 根据配置构造 zap.Logger
func New(cfg Config) (*zap.Logger, error) {
	if cfg.Level == "" {
		cfg.Level = "info"
	}
	if cfg.Output == "" {
		cfg.Output = "stdout"
	}

	level := zap.NewAtomicLevelAt(parseLevel(cfg.Level))

	// 编码器配置
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "ts"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderCfg.EncodeDuration = zapcore.SecondsDurationEncoder
	encoderCfg.EncodeLevel = zapcore.CapitalLevelEncoder

	var encoder zapcore.Encoder
	if cfg.Format == "console" {
		encoder = zapcore.NewConsoleEncoder(encoderCfg)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderCfg)
	}

	// 输出
	ws, err := openWriteSyncer(cfg.Output, cfg.Rotation)
	if err != nil {
		return nil, fmt.Errorf("open log output: %w", err)
	}

	core := zapcore.NewCore(encoder, ws, level)
	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(0), zap.AddStacktrace(zapcore.ErrorLevel))

	return logger, nil
}

func parseLevel(s string) zapcore.Level {
	var level zapcore.Level
	if err := level.UnmarshalText([]byte(s)); err != nil {
		return zapcore.InfoLevel
	}
	return level
}
