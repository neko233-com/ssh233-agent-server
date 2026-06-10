package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/neko233/ssh233-agent-server/internal/agent"
	"github.com/neko233/ssh233-agent-server/internal/api"
	"github.com/neko233/ssh233-agent-server/internal/audit"
	"github.com/neko233/ssh233-agent-server/internal/auth"
	"github.com/neko233/ssh233-agent-server/internal/bastion"
	"github.com/neko233/ssh233-agent-server/internal/config"
	"github.com/neko233/ssh233-agent-server/internal/models"
	"github.com/neko233/ssh233-agent-server/internal/store"
)

func TestHealth(t *testing.T) {
	_, router := newAPITest(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestLoginAndHostsCRUD(t *testing.T) {
	st, router := newAPITest(t)
	token := loginToken(t, router, "root", "root", "")

	tenant := &models.Tenant{Name: "TestCo", Slug: "testco", Enabled: true}
	if err := st.CreateTenant(tenant); err != nil {
		t.Fatal(err)
	}

	body := map[string]any{
		"name": "db1", "address": "192.168.1.10", "port": 22,
		"username": "root", "tenant_id": tenant.ID, "enabled": true,
	}
	rec := authRequest(t, router, http.MethodPost, "/api/v1/hosts", token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("create host: %d %s", rec.Code, rec.Body.String())
	}

	rec = authRequest(t, router, http.MethodGet, "/api/v1/hosts", token, nil)
	var hosts []models.Host
	json.Unmarshal(rec.Body.Bytes(), &hosts)
	if len(hosts) == 0 {
		t.Fatal("expected hosts")
	}

	// tenant-scoped user cannot see other tenant until assigned
	u := &models.User{TenantID: tenant.ID, Username: "op", Role: "operator"}
	_ = st.CreateUser(u, "op123")
	opToken := loginToken(t, router, "op", "op123", "testco")
	rec = authRequest(t, router, http.MethodGet, "/api/v1/hosts", opToken, nil)
	json.Unmarshal(rec.Body.Bytes(), &hosts)
	if len(hosts) != 1 {
		t.Fatalf("tenant user hosts: %d", len(hosts))
	}
}

func TestTenantsRootOnly(t *testing.T) {
	st, router := newAPITest(t)
	tenant := &models.Tenant{Name: "X", Slug: "xco", Enabled: true}
	_ = st.CreateTenant(tenant)
	_ = st.CreateUser(&models.User{TenantID: tenant.ID, Username: "u1", Role: "operator"}, "pw")
	opToken := loginToken(t, router, "u1", "pw", "xco")
	rec := authRequest(t, router, http.MethodGet, "/api/v1/tenants", opToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d", rec.Code)
	}
	rootToken := loginToken(t, router, "root", "root", "")
	rec = authRequest(t, router, http.MethodGet, "/api/v1/tenants", rootToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("root tenants: %d", rec.Code)
	}
}

func TestAuditStatsAndCleanupAPI(t *testing.T) {
	st, router := newAPITest(t)
	tenant, _ := st.GetTenantBySlug("default")
	for i := 0; i < 3; i++ {
		_ = st.WriteAudit(&models.AuditLog{
			TenantID: tenant.ID, Username: "root", Action: "test", Detail: "x",
		})
	}

	rootToken := loginToken(t, router, "root", "root", "")
	rec := authRequest(t, router, http.MethodGet, "/api/v1/audit/stats", rootToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("stats: %d %s", rec.Code, rec.Body.String())
	}
	var stats store.AuditStats
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Total < 3 {
		t.Fatalf("expected at least 3 audit rows, got %d", stats.Total)
	}

	op := &models.User{TenantID: tenant.ID, Username: "op2", Role: "operator"}
	_ = st.CreateUser(op, "pw")
	opToken := loginToken(t, router, "op2", "pw", "default")
	rec = authRequest(t, router, http.MethodDelete, "/api/v1/audit?all=true", opToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("operator should not cleanup: %d", rec.Code)
	}

	rec = authRequest(t, router, http.MethodDelete, "/api/v1/audit?all=true", rootToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("cleanup: %d %s", rec.Code, rec.Body.String())
	}
	var result map[string]int64
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["deleted"] < 1 {
		t.Fatalf("expected deletions, got %+v", result)
	}
}

func TestAgentRegister(t *testing.T) {
	st, router := newAPITest(t)
	tenant := &models.Tenant{Name: "A", Slug: "acme", Enabled: true}
	_ = st.CreateTenant(tenant)

	body := map[string]any{
		"name": "agent1", "register_token": "agent-register-token",
		"tenant_slug": "acme", "hostname": "n1", "ip": "10.0.0.2", "version": "1.0",
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", jsonBody(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
}

func newAPITest(t *testing.T) (*store.Store, http.Handler) {
	t.Helper()
	st, err := store.OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Bootstrap("root", "root"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	auditLog := audit.New(st)
	authSvc := auth.NewService(&cfg.Auth, st)
	bastionSrv, err := bastion.New(cfg, st, auditLog)
	if err != nil {
		t.Fatal(err)
	}
	agentMgr := agent.NewManager(cfg, st, auditLog)
	srv := api.New(st, authSvc, bastionSrv, agentMgr, auditLog)
	t.Cleanup(func() { st.Close() })
	return st, srv.Router()
}

func loginToken(t *testing.T, router http.Handler, user, pass, tenant string) string {
	t.Helper()
	body := map[string]string{"username": user, "password": pass}
	if tenant != "" {
		body["tenant_slug"] = tenant
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", jsonBody(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp["token"]
}

func authRequest(t *testing.T, router http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, jsonBody(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func jsonBody(v any) *bytes.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}
