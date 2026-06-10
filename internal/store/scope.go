package store

// Scope controls tenant isolation for queries.
type Scope struct {
	TenantID string
	Root     bool
}

func (s Scope) tenantFilter(column string) (string, []any) {
	if s.Root {
		return "1=1", nil
	}
	return column + " = ?", []any{s.TenantID}
}
