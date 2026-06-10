package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log/slog"
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
	"github.com/neko233/ssh233-agent-server/internal/store"
	"github.com/neko233/ssh233-agent-server/internal/version"
)

//go:embed all:static
var staticFS embed.FS

func main() {
	configPath := flag.String("config", "config.yaml", "config file path")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		println("ssh233-server", version.Version, version.Commit, version.Date)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	st, err := store.Open(&cfg.Database)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	if err := st.Bootstrap(cfg.Auth.AdminUser, cfg.Auth.AdminPassword); err != nil {
		slog.Error("bootstrap", "error", err)
		os.Exit(1)
	}

	auditLog := audit.New(st)
	authSvc := auth.NewService(&cfg.Auth, st)

	bastionSrv, err := bastion.New(cfg, st, auditLog)
	if err != nil {
		slog.Error("init bastion", "error", err)
		os.Exit(1)
	}

	agentMgr := agent.NewManager(cfg, st, auditLog)
	stop := make(chan struct{})
	go agentMgr.StartStaleChecker(stop)

	apiSrv := api.New(st, authSvc, bastionSrv, agentMgr, auditLog)
	apiHandler := apiSrv.Router()
	staticHandler := staticFileServer()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/health" {
			apiHandler.ServeHTTP(w, r)
			return
		}
		staticHandler.ServeHTTP(w, r)
	})

	httpServer := &http.Server{
		Addr:         cfg.Server.HTTPAddr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0,
	}

	go func() {
		slog.Info("HTTP server listening", "addr", cfg.Server.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server", "error", err)
			os.Exit(1)
		}
	}()

	go func() {
		if err := bastionSrv.ListenAndServe(); err != nil {
			slog.Error("ssh bastion", "error", err)
			os.Exit(1)
		}
	}()

	slog.Info("SSH233 Agent Server started",
		"http", cfg.Server.HTTPAddr,
		"ssh", cfg.Server.SSHAddr,
		"db", cfg.Database.Driver,
		"admin", cfg.Auth.AdminUser,
	)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	close(stop)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
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
