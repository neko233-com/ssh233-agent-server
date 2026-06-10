package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/neko233/ssh233-agent-server/internal/agent"
	"github.com/neko233/ssh233-agent-server/internal/api"
	"github.com/neko233/ssh233-agent-server/internal/audit"
	"github.com/neko233/ssh233-agent-server/internal/auth"
	"github.com/neko233/ssh233-agent-server/internal/bastion"
	"github.com/neko233/ssh233-agent-server/internal/config"
	"github.com/neko233/ssh233-agent-server/internal/store"
)

func TestHTTPServerSmoke(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Database.SQLite.Path = filepath.Join(dir, "test.db")
	cfg.SSH.HostKeyPath = filepath.Join(dir, "host_key")
	cfg.Auth.JWTSecret = "test-secret"

	st, err := store.Open(&cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Bootstrap("root", "root"); err != nil {
		t.Fatal(err)
	}

	auditLog := audit.New(st)
	authSvc := auth.NewService(&cfg.Auth, st)
	bastionSrv, err := bastion.New(cfg, st, auditLog)
	if err != nil {
		t.Fatal(err)
	}
	agentMgr := agent.NewManager(cfg, st, auditLog)
	handler := api.New(st, authSvc, bastionSrv, agentMgr, auditLog).Router()

	ts := httptest.NewServer(handler)
	defer ts.Close()

	waitHealth(t, ts.URL)

	token := login(t, ts.URL, "root", "root", "")
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tenants status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "default") {
		t.Fatalf("expected default tenant: %s", body)
	}
}

func waitHealth(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("health check timeout")
}

func login(t *testing.T, base, user, pass, tenant string) string {
	t.Helper()
	payload := map[string]string{"username": user, "password": pass}
	if tenant != "" {
		payload["tenant_slug"] = tenant
	}
	b, _ := json.Marshal(payload)
	resp, err := http.Post(base+"/api/v1/auth/login", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status %d", resp.StatusCode)
	}
	var out map[string]string
	json.NewDecoder(resp.Body).Decode(&out)
	return out["token"]
}
