package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/neko233/ssh233-agent-server/internal/config"
)

func TestDefaultRootAdmin(t *testing.T) {
	cfg := config.Default()
	if cfg.Auth.AdminUser != "root" || cfg.Auth.AdminPassword != "root" {
		t.Fatalf("expected root/root default admin")
	}
	if cfg.Database.Driver != "sqlite" {
		t.Fatalf("expected sqlite default driver")
	}
}

func TestLoadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
server:
  http_addr: ":9090"
database:
  driver: mysql
  mysql:
    dsn: "user:pass@tcp(localhost:3306)/db"
auth:
  token_ttl: 12h
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.HTTPAddr != ":9090" {
		t.Fatalf("http addr: %s", cfg.Server.HTTPAddr)
	}
	if cfg.Database.Driver != "mysql" {
		t.Fatalf("driver: %s", cfg.Database.Driver)
	}
	if cfg.Auth.TokenTTL.Duration() != 12*time.Hour {
		t.Fatalf("token ttl: %s", cfg.Auth.TokenTTL.Duration())
	}
}

func TestLoadMissingUsesDefault(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.HTTPAddr != ":6030" {
		t.Fatal("expected defaults")
	}
}

func TestResolveLogPath(t *testing.T) {
	cfg := config.Default()
	cfg.Logging.Path = "logs/app.log"
	got := cfg.ResolveLogPath("/etc/ssh233")
	if got != filepath.Join("/etc/ssh233", "logs/app.log") {
		t.Fatalf("relative path: %s", got)
	}
	absLog := filepath.Join(t.TempDir(), "abs.log")
	cfg.Logging.Path = absLog
	if cfg.ResolveLogPath("/etc/ssh233") != absLog {
		t.Fatal("absolute path should pass through")
	}
}

func TestDefaultLogging(t *testing.T) {
	cfg := config.Default()
	if cfg.Logging.MaxSizeMB != 10 || cfg.Logging.MaxBackups != 5 {
		t.Fatalf("logging defaults: %+v", cfg.Logging)
	}
}
