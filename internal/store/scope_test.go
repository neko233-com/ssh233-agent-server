package store_test

import "testing"

func TestScopeTenantFilter(t *testing.T) {
	s := storeScope{TenantID: "tid", Root: false}
	where, args := s.tenantFilter("tenant_id")
	if where != "tenant_id = ?" || len(args) != 1 || args[0] != "tid" {
		t.Fatalf("unexpected filter: %s %v", where, args)
	}
	root := storeScope{Root: true}
	where, args = root.tenantFilter("tenant_id")
	if where != "1=1" || len(args) != 0 {
		t.Fatalf("root filter: %s", where)
	}
}

type storeScope struct {
	TenantID string
	Root     bool
}

func (s storeScope) tenantFilter(column string) (string, []any) {
	if s.Root {
		return "1=1", nil
	}
	return column + " = ?", []any{s.TenantID}
}
