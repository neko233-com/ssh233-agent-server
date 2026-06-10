package auth_test

import (
	"testing"

	"github.com/neko233/ssh233-agent-server/internal/auth"
	"github.com/neko233/ssh233-agent-server/internal/config"
	"github.com/neko233/ssh233-agent-server/internal/models"
	"github.com/neko233/ssh233-agent-server/internal/store"
)

func TestLoginRoot(t *testing.T) {
	_, svc := newAuthTest(t)
	token, claims, err := svc.Login("root", "root", "")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || !claims.IsRoot() {
		t.Fatalf("claims: %+v", claims)
	}
}

func TestLoginTenantUser(t *testing.T) {
	st, svc := newAuthTest(t)
	tenant := &models.Tenant{Name: "Demo", Slug: "demo", Enabled: true}
	if err := st.CreateTenant(tenant); err != nil {
		t.Fatal(err)
	}
	u := &models.User{TenantID: tenant.ID, Username: "alice", Role: "admin"}
	if err := st.CreateUser(u, "pass123"); err != nil {
		t.Fatal(err)
	}
	token, claims, err := svc.Login("alice", "pass123", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || claims.TenantID != tenant.ID {
		t.Fatalf("claims: %+v", claims)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	_, svc := newAuthTest(t)
	_, _, err := svc.Login("root", "wrong", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseTokenRoundTrip(t *testing.T) {
	_, svc := newAuthTest(t)
	token, _, err := svc.Login("root", "root", "")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := svc.ParseToken(token)
	if err != nil || !claims.IsRoot() {
		t.Fatalf("parse: %+v err=%v", claims, err)
	}
}

func newAuthTest(t *testing.T) (*store.Store, *auth.Service) {
	t.Helper()
	st, err := store.OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Bootstrap("root", "root"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	svc := auth.NewService(&cfg.Auth, st)
	t.Cleanup(func() { st.Close() })
	return st, svc
}
