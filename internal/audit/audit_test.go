package audit_test

import (
	"testing"

	"github.com/neko233/ssh233-agent-server/internal/audit"
	"github.com/neko233/ssh233-agent-server/internal/store"
)

func TestAuditLogWritten(t *testing.T) {
	st, err := store.OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Bootstrap("root", "root"); err != nil {
		t.Fatal(err)
	}

	logger := audit.New(st)
	logger.Log("tid", "test_action", "root", "uid", "hid", "sid", "detail", "127.0.0.1")

	logs, err := st.ListAuditLogs(store.Scope{Root: true}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Action != "test_action" {
		t.Fatalf("audit logs: %+v", logs)
	}
}
