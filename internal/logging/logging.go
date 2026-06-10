package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Config struct {
	Path       string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Level      string
}

func Setup(cfg Config) (*slog.Logger, func() error, error) {
	if strings.TrimSpace(cfg.Path) == "" {
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: parseLevel(cfg.Level)}))
		slog.SetDefault(logger)
		return logger, func() error { return nil }, nil
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o755); err != nil {
		return nil, nil, err
	}
	maxSize := int64(cfg.MaxSizeMB) * 1024 * 1024
	if maxSize <= 0 {
		maxSize = 10 * 1024 * 1024
	}
	maxBackups := cfg.MaxBackups
	if maxBackups <= 0 {
		maxBackups = 5
	}
	writer, err := newRotatingFileWriter(cfg.Path, maxSize, maxBackups)
	if err != nil {
		return nil, nil, err
	}
	level := parseLevel(cfg.Level)
	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{Level: level})
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger, writer.Close, nil
}

func parseLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type rotatingFileWriter struct {
	mu         sync.Mutex
	path       string
	file       *os.File
	size       int64
	maxSize    int64
	maxBackups int
}

func newRotatingFileWriter(path string, maxSize int64, maxBackups int) (*rotatingFileWriter, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &rotatingFileWriter{
		path: path, file: file, size: info.Size(),
		maxSize: maxSize, maxBackups: maxBackups,
	}, nil
}

func (w *rotatingFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.rotateIfNeeded(int64(len(p))); err != nil {
		return 0, err
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *rotatingFileWriter) rotateIfNeeded(incoming int64) error {
	if w.file == nil {
		return fmt.Errorf("log writer is closed")
	}
	if w.size+incoming <= w.maxSize {
		return nil
	}
	if err := w.file.Close(); err != nil {
		return err
	}
	w.file = nil
	if err := w.shiftBackups(); err != nil {
		return err
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	w.file = file
	w.size = 0
	return nil
}

func (w *rotatingFileWriter) shiftBackups() error {
	oldest := w.path + fmt.Sprintf(".%d", w.maxBackups)
	_ = os.Remove(oldest)
	for i := w.maxBackups - 1; i >= 1; i-- {
		src := w.path + fmt.Sprintf(".%d", i)
		dst := w.path + fmt.Sprintf(".%d", i+1)
		_ = os.Rename(src, dst)
	}
	if _, err := os.Stat(w.path); err == nil {
		if err := os.Rename(w.path, w.path+".1"); err != nil {
			return err
		}
	}
	return nil
}

var _ io.WriteCloser = (*rotatingFileWriter)(nil)
