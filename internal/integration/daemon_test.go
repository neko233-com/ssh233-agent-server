package integration_test

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDaemonStartStopStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon integration in -short mode")
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")

	data := t.TempDir()
	httpPort := pickFreePort(t)
	sshPort := pickFreePort(t)
	cfgPath := filepath.Join(data, "config.yaml")
	logPath := filepath.Join(data, "logs", "app.log")
	writeSmokeConfig(t, cfgPath, data, logPath, httpPort, sshPort)

	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	bin := filepath.Join(data, "ssh233-server"+ext)
	build := exec.Command("go", "build", "-o", bin, "./cmd/server")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := run("start", "-config", cfgPath); err != nil {
		t.Fatalf("start failed: %v\n%s", err, out)
	}
	t.Cleanup(func() { _, _ = run("stop", "-config", cfgPath) })

	deadline := time.Now().Add(20 * time.Second)
	var healthy bool
	base := "http://127.0.0.1:" + httpPort
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/health")
		if err == nil {
			var body map[string]string
			_ = json.NewDecoder(resp.Body).Decode(&body)
			_ = resp.Body.Close()
			if body["status"] == "ok" {
				healthy = true
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !healthy {
		t.Fatal("daemon never became healthy")
	}

	statusOut, err := run("status", "-config", cfgPath)
	if err != nil {
		t.Fatalf("status: %v\n%s", err, statusOut)
	}
	if !strings.Contains(statusOut, "status=running") {
		t.Fatalf("expected running status:\n%s", statusOut)
	}
	if !strings.Contains(statusOut, "autostart_enabled=false") {
		t.Fatalf("autostart should be disabled by default:\n%s", statusOut)
	}

	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("rotating log not created: %v", err)
	}

	if out, err := run("stop", "-config", cfgPath); err != nil {
		t.Fatalf("stop: %v\n%s", err, out)
	}

	stopped := true
	for i := 0; i < 30; i++ {
		if _, err := http.Get(base + "/health"); err != nil {
			stopped = true
			break
		}
		stopped = false
		time.Sleep(200 * time.Millisecond)
	}
	if !stopped {
		t.Fatal("server still responding after stop")
	}
}

func writeSmokeConfig(t *testing.T, cfgPath, dataDir, logPath, httpPort, sshPort string) {
	t.Helper()
	content := `server:
  http_addr: "127.0.0.1:` + httpPort + `"
  ssh_addr: "127.0.0.1:` + sshPort + `"
database:
  driver: sqlite
  sqlite:
    path: ` + filepath.ToSlash(filepath.Join(dataDir, "test.db")) + `
auth:
  jwt_secret: integration-secret
  token_ttl: 1h
  admin_user: root
  admin_password: root
ssh:
  host_key_path: ` + filepath.ToSlash(filepath.Join(dataDir, "host_key")) + `
agent:
  register_token: integration-token
  heartbeat_ttl: 60s
logging:
  path: ` + filepath.ToSlash(logPath) + `
  max_size_mb: 1
  max_backups: 2
  level: info
`
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func pickFreePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()
	if err != nil {
		t.Fatal(err)
	}
	return port
}
