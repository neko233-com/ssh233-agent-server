package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestBrowserURL(t *testing.T) {
	cases := map[string]string{
		":6030":        "http://127.0.0.1:6030",
		"127.0.0.1:80": "http://127.0.0.1:80",
		"0.0.0.0:8080": "http://127.0.0.1:8080",
	}
	for in, want := range cases {
		if got := browserURL(in); got != want {
			t.Fatalf("browserURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHealthOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	host := srv.Listener.Addr().String()
	if !healthOK(host) {
		t.Fatalf("healthOK(%q) expected true", host)
	}
	if healthOK("127.0.0.1:1") {
		t.Fatal("expected false for closed port")
	}
}

func TestRuntimeStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  http_addr: \":0\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	st := runtimeState{
		PID:          os.Getpid(),
		HTTPAddr:     srv.Listener.Addr().String(),
		ConfigPath:   cfgPath,
		ControlToken: "test-token",
		StartedAt:    "now",
		Version:      "test",
	}
	if err := saveRuntimeState(cfgPath, st); err != nil {
		t.Fatal(err)
	}
	loaded, _, ok, err := loadRuntimeState(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected running state")
	}
	if loaded.PID != st.PID || loaded.ControlToken != st.ControlToken {
		t.Fatalf("state mismatch: %+v vs %+v", loaded, st)
	}
	cleanupRuntimeState(cfgPath, st.PID)
	_, _, ok, err = loadRuntimeState(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected stale state after cleanup")
	}
}

func TestResolveConfigPath(t *testing.T) {
	if resolveConfigPath("") != "config.yaml" {
		t.Fatal("empty should default to config.yaml")
	}
	if resolveConfigPath("/tmp/x.yaml") != "/tmp/x.yaml" {
		t.Fatal("explicit path preserved")
	}
}
