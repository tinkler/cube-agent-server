package log

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// closeWS 试图关闭底层 writer(比如 *lumberjack.Logger),忽略类型不匹配
// Windows 上不主动 Close() 的话,temp 目录清理会报 "file in use"
func closeWS(t *testing.T, ws zapcore.WriteSyncer) {
	t.Helper()
	if c, ok := ws.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil {
			t.Logf("close underlying writer: %v", err)
		}
	}
}

// TestOpenWriteSyncer_Stdout 验证 stdout/stderr 分支不报错且能写
func TestOpenWriteSyncer_Stdout(t *testing.T) {
	for _, out := range []string{"stdout", "", "stderr"} {
		ws, err := openWriteSyncer(out, Rotation{})
		if err != nil {
			t.Fatalf("openWriteSyncer(%q): %v", out, err)
		}
		if _, err := ws.Write([]byte("test\n")); err != nil {
			if out == "stderr" {
				continue // stderr 在测试环境可能写不进(没 TTY)
			}
			t.Fatalf("ws.Write(%q): %v", out, err)
		}
		_ = ws.Sync()
	}
}

// TestOpenWriteSyncer_File_NoRotation 验证 Rotation 全 0 走 OpenFile 路径
func TestOpenWriteSyncer_File_NoRotation(t *testing.T) {
	dir, err := os.MkdirTemp("", "logtest-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "agent.log")
	ws, err := openWriteSyncer(p, Rotation{})
	if err != nil {
		t.Fatalf("openWriteSyncer: %v", err)
	}
	defer closeWS(t, ws)

	logger := zap.New(zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), ws, zap.InfoLevel))
	logger.Info("hello", zap.String("k", "v"))
	_ = logger.Sync()

	if _, err := os.Stat(p); err != nil {
		t.Fatalf("expected log file %s to exist: %v", p, err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(b), `"msg":"hello"`) {
		t.Fatalf("log content missing msg: %s", b)
	}
}

// TestOpenWriteSyncer_File_Rotation 验证 Rotation 配齐后走 lumberjack,
// 自动创建父目录 + 写超过 MaxSize 时自动 rotate 出 .log-<timestamp> 文件
func TestOpenWriteSyncer_File_Rotation(t *testing.T) {
	dir, err := os.MkdirTemp("", "logrot-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	// 用嵌套目录验证 MkdirAll
	p := filepath.Join(dir, "nested", "logs", "agent.log")
	ws, err := openWriteSyncer(p, Rotation{
		MaxSizeMB:  1, // 1MB 触发 rotate
		MaxAgeDays: 7,
		MaxBackups: 3,
		Compress:   false,
	})
	if err != nil {
		t.Fatalf("openWriteSyncer: %v", err)
	}
	defer closeWS(t, ws)

	logger := zap.New(zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), ws, zap.InfoLevel))
	// 写 ~3MB(>1MB 阈值),期望至少 2 个文件:agent.log + agent.log-<ts>.log
	payload := strings.Repeat("x", 8000)
	for i := 0; i < 500; i++ {
		logger.Info("payload", zap.Int("i", i), zap.String("pad", payload))
	}
	_ = logger.Sync()

	entries, err := os.ReadDir(filepath.Dir(p))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	var logFiles int
	for _, e := range entries {
		// lumberjack 命名:agent.log(当前) + agent-2026-08-18T23-04-12.606.log(已切)
		if !e.IsDir() && (e.Name() == "agent.log" || strings.HasPrefix(e.Name(), "agent-")) {
			logFiles++
		}
	}
	if logFiles < 2 {
		files := make([]string, 0, len(entries))
		for _, e := range entries {
			files = append(files, e.Name())
		}
		t.Fatalf("expected ≥2 log files after rotation, got %d, files=%v", logFiles, files)
	}
	t.Logf("rotation ok: %d files in %s", logFiles, filepath.Dir(p))
}
