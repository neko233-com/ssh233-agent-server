package sshkeys_test

import (
	"strings"
	"testing"

	"github.com/neko233/ssh233-agent-server/internal/sshkeys"
)

func TestGenerateAndParse(t *testing.T) {
	kp, err := sshkeys.Generate("test")
	if err != nil {
		t.Fatal(err)
	}
	if kp.PublicKey == "" || kp.PrivateKey == "" || kp.Fingerprint == "" {
		t.Fatal("incomplete keypair")
	}
	if !strings.HasPrefix(kp.Fingerprint, "SHA256:") {
		t.Fatalf("fingerprint: %s", kp.Fingerprint)
	}
	parsed, err := sshkeys.ParsePrivateKey(kp.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Fingerprint != kp.Fingerprint {
		t.Fatal("fingerprint mismatch after parse")
	}
}

func TestTrimNewline(t *testing.T) {
	// exercised via UploadPublicKey path indirectly; verify Generate pub key ends with newline
	kp, _ := sshkeys.Generate("t")
	if !strings.HasSuffix(kp.PublicKey, "\n") {
		t.Fatal("authorized key format should include trailing newline")
	}
}
