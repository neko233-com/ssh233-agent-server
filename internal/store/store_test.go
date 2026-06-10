package store_test

import (
	"testing"

	"github.com/neko233/ssh233-agent-server/internal/models"
	"github.com/neko233/ssh233-agent-server/internal/store"
)

func TestBootstrapCreatesRootAndDefaultTenant(t *testing.T) {
	st, err := store.OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.Bootstrap("root", "root"); err != nil {
		t.Fatal(err)
	}
	root, err := st.GetRootUser("root")
	if err != nil || root == nil || root.Role != models.RoleRoot {
		t.Fatalf("root user missing: %v", root)
	}
	tenant, err := st.GetTenantBySlug("default")
	if err != nil || tenant == nil {
		t.Fatal("default tenant missing")
	}
}

func TestTenantIsolationHosts(t *testing.T) {
	st := newTestStore(t)
	tenantA := createTenant(t, st, "acme", "acme")
	tenantB := createTenant(t, st, "beta", "beta")

	h1 := &models.Host{TenantID: tenantA.ID, Name: "web", Address: "10.0.0.1", Enabled: true}
	h2 := &models.Host{TenantID: tenantB.ID, Name: "web", Address: "10.0.0.2", Enabled: true}
	_ = st.CreateHost(h1)
	_ = st.CreateHost(h2)

	listA, err := st.ListHosts(store.Scope{TenantID: tenantA.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(listA) != 1 || listA[0].Address != "10.0.0.1" {
		t.Fatalf("tenant A hosts: %+v", listA)
	}

	all, err := st.ListHosts(store.Scope{Root: true})
	if err != nil || len(all) != 2 {
		t.Fatalf("root should see all hosts: %d", len(all))
	}
}

func TestCreateUserWithinTenant(t *testing.T) {
	st := newTestStore(t)
	tenant := createTenant(t, st, "corp", "corp")
	u := &models.User{TenantID: tenant.ID, Username: "alice", Role: "admin"}
	if err := st.CreateUser(u, "secret"); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetUserByUsername(tenant.ID, "alice")
	if err != nil || got == nil || got.Username != "alice" {
		t.Fatal("user not found in tenant")
	}
	if !st.VerifyPassword(got, "secret") {
		t.Fatal("password verify failed")
	}
}

func TestAuditStatsAndCleanup(t *testing.T) {
	st := newTestStore(t)
	tenant := createTenant(t, st, "auditco", "auditco")
	scope := store.Scope{TenantID: tenant.ID}

	for i := 0; i < 5; i++ {
		if err := st.WriteAudit(&models.AuditLog{
			TenantID: tenant.ID, Username: "u", Action: "test", Detail: "d",
		}); err != nil {
			t.Fatal(err)
		}
	}

	stats, err := st.AuditStats(scope)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 5 {
		t.Fatalf("total: %d", stats.Total)
	}

	deleted, err := st.DeleteAllAudit(scope)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 5 {
		t.Fatalf("deleted: %d", deleted)
	}
	stats, err = st.AuditStats(scope)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 0 {
		t.Fatalf("expected empty after cleanup, got %d", stats.Total)
	}
}

func TestHostKeyPairUpsert(t *testing.T) {
	st := newTestStore(t)
	tenant := createTenant(t, st, "t1", "t1")
	h := &models.Host{TenantID: tenant.ID, Name: "srv", Address: "1.1.1.1", Enabled: true}
	_ = st.CreateHost(h)
	u := &models.User{TenantID: tenant.ID, Username: "u", Role: "operator"}
	_ = st.CreateUser(u, "x")

	kp := &models.HostKeyPair{UserID: u.ID, HostID: h.ID, PublicKey: "pub", PrivateKey: "priv", Fingerprint: "fp", Uploaded: true}
	if err := st.SaveHostKeyPair(kp); err != nil {
		t.Fatal(err)
	}
	kp2 := &models.HostKeyPair{UserID: u.ID, HostID: h.ID, PublicKey: "pub2", PrivateKey: "priv2", Fingerprint: "fp2", Uploaded: true}
	if err := st.SaveHostKeyPair(kp2); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetHostKeyPair(u.ID, h.ID)
	if err != nil || got.PublicKey != "pub2" {
		t.Fatalf("upsert failed: %+v", got)
	}
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Bootstrap("root", "root"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func createTenant(t *testing.T, st *store.Store, name, slug string) *models.Tenant {
	t.Helper()
	tenant := &models.Tenant{Name: name, Slug: slug, Enabled: true}
	if err := st.CreateTenant(tenant); err != nil {
		t.Fatal(err)
	}
	return tenant
}
