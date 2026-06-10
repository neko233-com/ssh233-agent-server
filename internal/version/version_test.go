package version_test

import (
	"testing"

	"github.com/neko233/ssh233-agent-server/internal/version"
)

func TestStringDefault(t *testing.T) {
	if version.String() == "" {
		t.Fatal("version string should not be empty")
	}
}
