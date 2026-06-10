package logging_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neko233/ssh233-agent-server/internal/logging"
)

func TestSetupWithFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	logger, closeFn, err := logging.Setup(logging.Config{
		Path: path, MaxSizeMB: 1, MaxBackups: 2, Level: "info",
	})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("hello smoke")
	_ = closeFn()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestSetupStdout(t *testing.T) {
	_, closeFn, err := logging.Setup(logging.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = closeFn()
}

func TestLogRotationCreatesBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rotate.log")
	logger, closeFn, err := logging.Setup(logging.Config{
		Path: path, MaxSizeMB: 1, MaxBackups: 2, Level: "info",
	})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 8192)
	for i := 0; i < 160; i++ {
		logger.Info(line)
	}
	if err := closeFn(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("expected rotated backup: %v", err)
	}
}
