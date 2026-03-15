package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Level string
	Path  string
}

type Logger struct {
	base *slog.Logger
}

func New(cfg Config) (*Logger, error) {
	level := parseLevel(cfg.Level)

	var writer io.Writer = os.Stdout
	if cfg.Path != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o700); err != nil {
			return nil, err
		}

		file, err := os.OpenFile(cfg.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, err
		}
		writer = io.MultiWriter(os.Stdout, file)
	}

	handler := slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level})
	return &Logger{base: slog.New(handler)}, nil
}

func (l *Logger) With(kv ...any) *Logger {
	return &Logger{base: l.base.With(kv...)}
}

func (l *Logger) Debug(msg string, kv ...any) {
	l.base.Debug(msg, kv...)
}

func (l *Logger) Info(msg string, kv ...any) {
	l.base.Info(msg, kv...)
}

func (l *Logger) Warn(msg string, kv ...any) {
	l.base.Warn(msg, kv...)
}

func (l *Logger) Error(msg string, kv ...any) {
	l.base.Error(msg, kv...)
}

func parseLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
