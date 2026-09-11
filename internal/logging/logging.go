// Package logging 负责把 config.log.* 真正接进 slog handler。
// 之前 cfg.Log 从未被消费，导致 LOG_LEVEL 形同虚设（ROADMAP §1.2 / M0-8）。
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/fakemby/fakemby/internal/config"
)

// Setup 根据配置构造全局 logger。
// 支持两个输出目标：stderr（始终）与可选的日志文件（log.file）。
// 级别非法时回落 info。
func Setup(cfg *config.Config) (*slog.Logger, error) {
	level := parseLevel(cfg.Log.Level)

	opts := &slog.HandlerOptions{Level: level}

	var writers []io.Writer
	writers = append(writers, os.Stderr)

	if cfg.Log.File != "" {
		f, err := os.OpenFile(cfg.Log.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, err
		}
		writers = append(writers, f)
	}

	var w io.Writer = writers[0]
	if len(writers) > 1 {
		w = io.MultiWriter(writers...)
	}

	logger := slog.New(slog.NewTextHandler(w, opts))
	slog.SetDefault(logger)
	return logger, nil
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info", "":
		return slog.LevelInfo
	default:
		slog.Warn("未知的日志级别，回落为 info", "level", s)
		return slog.LevelInfo
	}
}
