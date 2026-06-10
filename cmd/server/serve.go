package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/neko233/ssh233-agent-server/internal/agent"
	"github.com/neko233/ssh233-agent-server/internal/api"
	"github.com/neko233/ssh233-agent-server/internal/audit"
	"github.com/neko233/ssh233-agent-server/internal/auth"
	"github.com/neko233/ssh233-agent-server/internal/bastion"
	"github.com/neko233/ssh233-agent-server/internal/config"
	"github.com/neko233/ssh233-agent-server/internal/logging"
	"github.com/neko233/ssh233-agent-server/internal/store"
	"github.com/neko233/ssh233-agent-server/internal/version"
)

//go:embed all:static
var staticFS embed.FS

type runtimeState struct {
	PID          int    `json:"pid"`
	HTTPAddr     string `json:"http_addr"`
	ConfigPath   string `json:"config_path"`
	ControlToken string `json:"control_token"`
	StartedAt    string `json:"started_at"`
	Version      string `json:"version"`
}

func runServe(configPath string) error {
	cfgPath := resolveConfigPath(configPath)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	dir := configDir(cfgPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	logPath := cfg.ResolveLogPath(dir)
	logger, closeLog, err := logging.Setup(logging.Config{
		Path:       logPath,
		MaxSizeMB:  cfg.Logging.MaxSizeMB,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAgeDays: cfg.Logging.MaxAgeDays,
		Level:      cfg.Logging.Level,
	})
	if err != nil {
		return fmt.Errorf("setup logger: %w", err)
	}
	defer func() { _ = closeLog() }()

	st, err := store.Open(&cfg.Database)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	if err := st.Bootstrap(cfg.Auth.AdminUser, cfg.Auth.AdminPassword); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	controlToken, err := randomHex(16)
	if err != nil {
		return err
	}
	state := runtimeState{
		PID:          os.Getpid(),
		HTTPAddr:     cfg.Server.HTTPAddr,
		ConfigPath:   cfgPath,
		ControlToken: controlToken,
		StartedAt:    time.Now().Format(time.RFC3339),
		Version:      version.Version,
	}
	if err := saveRuntimeState(cfgPath, state); err != nil {
		return err
	}
	defer cleanupRuntimeState(cfgPath, state.PID)

	auditLog := audit.New(st)
	authSvc := auth.NewService(&cfg.Auth, st)

	bastionSrv, err := bastion.New(cfg, st, auditLog)
	if err != nil {
		return fmt.Errorf("init bastion: %w", err)
	}

	agentMgr := agent.NewManager(cfg, st, auditLog)
	stop := make(chan struct{})
	go agentMgr.StartStaleChecker(stop)

	apiSrv := api.New(st, authSvc, bastionSrv, agentMgr, auditLog)
	apiHandler := apiSrv.Router()
	staticHandler := staticFileServer()

	rootHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/health" {
			apiHandler.ServeHTTP(w, r)
			return
		}
		staticHandler.ServeHTTP(w, r)
	})

	mux := http.NewServeMux()
	shutdownCh := make(chan struct{}, 1)
	mux.HandleFunc("/__admin/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if !authorizeControlRequest(r, controlToken) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		select {
		case shutdownCh <- struct{}{}:
		default:
		}
	})
	mux.Handle("/", rootHandler)

	httpServer := &http.Server{
		Addr:         cfg.Server.HTTPAddr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0,
	}

	go func() {
		logger.Info("HTTP server listening", "addr", cfg.Server.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server", "error", err)
			select {
			case shutdownCh <- struct{}{}:
			default:
			}
		}
	}()

	go func() {
		if err := bastionSrv.ListenAndServe(); err != nil {
			logger.Error("ssh bastion stopped", "error", err)
		}
	}()

	logger.Info("SSH233 Agent Server started",
		"version", version.Version,
		"http", cfg.Server.HTTPAddr,
		"ssh", cfg.Server.SSHAddr,
		"db", cfg.Database.Driver,
		"admin", cfg.Auth.AdminUser,
		"config", cfgPath,
	)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sig:
	case <-shutdownCh:
	}

	close(stop)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
	logger.Info("server stopped")
	return nil
}

func authorizeControlRequest(r *http.Request, token string) bool {
	if r.Method != http.MethodPost {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host != "127.0.0.1" && host != "::1" {
		return false
	}
	return r.Header.Get("X-SSH233-Control-Token") == token
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func saveRuntimeState(configPath string, st runtimeState) error {
	if err := os.MkdirAll(runtimeDir(configPath), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(runtimeStatePath(configPath), append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(runtimePIDPath(configPath), []byte(fmt.Sprintf("%d\n", st.PID)), 0o644)
}

func loadRuntimeState(configPath string) (runtimeState, string, bool, error) {
	path := runtimeStatePath(configPath)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return runtimeState{}, path, false, nil
	}
	if err != nil {
		return runtimeState{}, path, false, err
	}
	var st runtimeState
	if err := json.Unmarshal(b, &st); err != nil {
		return runtimeState{}, path, false, err
	}
	if strings.TrimSpace(st.HTTPAddr) == "" || !healthOK(st.HTTPAddr) {
		return st, path, false, nil
	}
	return st, path, true, nil
}

func cleanupRuntimeState(configPath string, pid int) {
	b, err := os.ReadFile(runtimePIDPath(configPath))
	if err == nil && strings.TrimSpace(string(b)) != fmt.Sprintf("%d", pid) {
		return
	}
	_ = os.Remove(runtimeStatePath(configPath))
	_ = os.Remove(runtimePIDPath(configPath))
}

func healthOK(addr string) bool {
	client := &http.Client{Timeout: 800 * time.Millisecond}
	resp, err := client.Get(browserURL(addr) + "/health")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode == http.StatusOK && strings.Contains(string(body), "ok")
}

func browserURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "6030"
	}
	return fmt.Sprintf("http://%s", net.JoinHostPort(host, port))
}

func staticFileServer() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			http.Redirect(w, r, "/login.html", http.StatusFound)
			return
		}
		if path == "/manager" || path == "/manager/" {
			http.Redirect(w, r, "/manager.html", http.StatusFound)
			return
		}
		if !fileExists(sub, path) {
			http.NotFound(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func fileExists(fsys fs.FS, path string) bool {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return true
	}
	_, err := fs.Stat(fsys, path)
	return err == nil
}
