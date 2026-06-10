package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/neko233/ssh233-agent-server/internal/agent"
	"github.com/neko233/ssh233-agent-server/internal/audit"
	"github.com/neko233/ssh233-agent-server/internal/auth"
	"github.com/neko233/ssh233-agent-server/internal/bastion"
	"github.com/neko233/ssh233-agent-server/internal/models"
	"github.com/neko233/ssh233-agent-server/internal/store"
	"github.com/neko233/ssh233-agent-server/internal/webssh"
	"golang.org/x/crypto/ssh"
)

type Server struct {
	store   *store.Store
	auth    *auth.Service
	bastion *bastion.Server
	agent   *agent.Manager
	audit   *audit.Logger
	webSSH  *webssh.Handler
}

func New(st *store.Store, authSvc *auth.Service, b *bastion.Server, am *agent.Manager, auditLog *audit.Logger) *Server {
	return &Server{
		store:   st,
		auth:    authSvc,
		bastion: b,
		agent:   am,
		audit:   auditLog,
		webSSH:  webssh.NewHandler(st, b, auditLog),
	}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"status": "ok"})
	})

	r.Post("/api/v1/auth/login", s.login)
	r.Post("/api/v1/agents/register", s.agent.Register)
	r.Post("/api/v1/agents/heartbeat", s.agent.Heartbeat)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(s.auth.Middleware)

		r.Get("/me", s.me)

		r.With(s.auth.RootMiddleware).Get("/tenants", s.listTenants)
		r.With(s.auth.RootMiddleware).Post("/tenants", s.createTenant)
		r.With(s.auth.RootMiddleware).Put("/tenants/{id}", s.updateTenant)

		r.Get("/hosts", s.listHosts)
		r.Post("/hosts", s.createHost)
		r.Get("/hosts/{id}", s.getHost)
		r.Put("/hosts/{id}", s.updateHost)
		r.Delete("/hosts/{id}", s.deleteHost)

		r.With(s.auth.AdminMiddleware).Get("/users", s.listUsers)
		r.With(s.auth.AdminMiddleware).Post("/users", s.createUser)

		r.Get("/agents", s.listAgents)
		r.Get("/sessions", s.listSessions)
		r.Get("/sessions/active", s.activeSessions)
		r.Get("/audit", s.listAudit)

		r.Post("/exec", s.execCommand)
		r.Post("/hosts/{id}/exec", s.execOnHost)
		r.Get("/webssh", s.webSSHConnect)
	})

	return r
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		TenantSlug string `json:"tenant_slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	token, claims, err := s.auth.Login(req.Username, req.Password, req.TenantSlug)
	if err != nil {
		http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]any{
		"token":       token,
		"user_id":     claims.UserID,
		"username":    claims.Username,
		"role":        claims.Role,
		"tenant_id":   claims.TenantID,
		"tenant_slug": claims.TenantSlug,
	})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, auth.ClaimsFromContext(r.Context()))
}

func (s *Server) listTenants(w http.ResponseWriter, r *http.Request) {
	tenants, err := s.store.ListTenants()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, tenants)
}

func (s *Server) createTenant(w http.ResponseWriter, r *http.Request) {
	var t models.Tenant
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	t.Enabled = true
	if err := s.store.CreateTenant(&t); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, t)
}

func (s *Server) updateTenant(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := s.store.GetTenant(id)
	if err != nil || t == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	if err := json.NewDecoder(r.Body).Decode(t); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	t.ID = id
	if err := s.store.UpdateTenant(t); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, t)
}

func (s *Server) listHosts(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	hosts, err := s.store.ListHosts(claims.Scope())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, hosts)
}

func (s *Server) createHost(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	var h models.Host
	if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	h.Enabled = true
	if !claims.IsRoot() {
		h.TenantID = claims.TenantID
	}
	if h.TenantID == "" {
		h.TenantID = claims.TenantID
	}
	if err := s.store.CreateHost(&h); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit.Log(h.TenantID, "host_create", claims.Username, claims.UserID, h.ID, "", h.Name, r.RemoteAddr)
	writeJSON(w, h)
}

func (s *Server) getHost(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	h, err := s.store.GetHost(claims.Scope(), chi.URLParam(r, "id"))
	if err != nil || h == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, h)
}

func (s *Server) updateHost(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	h, err := s.store.GetHost(claims.Scope(), id)
	if err != nil || h == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	if err := json.NewDecoder(r.Body).Decode(h); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	h.ID = id
	if err := s.store.UpdateHost(h); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, h)
}

func (s *Server) deleteHost(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if err := s.store.DeleteHost(claims.Scope(), chi.URLParam(r, "id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	users, err := s.store.ListUsers(claims.Scope())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, users)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
		TenantID string `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	if req.Role == "" {
		req.Role = "operator"
	}
	u := &models.User{
		Username: req.Username,
		Role:     req.Role,
		TenantID: claims.TenantID,
	}
	if claims.IsRoot() && req.TenantID != "" {
		u.TenantID = req.TenantID
	}
	if err := s.store.CreateUser(u, req.Password); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, u)
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	agents, err := s.store.ListAgents(claims.Scope())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, agents)
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	sessions, err := s.store.ListSessions(claims.Scope(), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, sessions)
}

func (s *Server) activeSessions(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	sessions, err := s.store.GetActiveSessions(claims.Scope())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, sessions)
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	logs, err := s.store.ListAuditLogs(claims.Scope(), limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, logs)
}

func (s *Server) execCommand(w http.ResponseWriter, r *http.Request) {
	s.execOnHost(w, r)
}

func (s *Server) execOnHost(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	user, _ := s.store.GetUserByID(claims.UserID)

	var req struct {
		HostID   string `json:"host_id"`
		Command  string `json:"command"`
		Password string `json:"password,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	if req.HostID == "" {
		req.HostID = chi.URLParam(r, "id")
	}

	host, err := s.store.GetHost(claims.Scope(), req.HostID)
	if err != nil || host == nil {
		http.Error(w, `{"error":"host not found"}`, http.StatusNotFound)
		return
	}

	resp, err := s.agent.ExecViaSSH(user, host, req.Command, req.Password, func(u *models.User, h *models.Host, pw string) (*ssh.Client, error) {
		if pw != "" {
			return s.bastion.ConnectWithPassword(u, h, pw)
		}
		client, err := s.bastion.ConnectWithKey(u, h)
		if err != nil {
			return nil, fmt.Errorf("no key for host %q — provide password on first login", h.Name)
		}
		return client, nil
	})
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	s.audit.Log(host.TenantID, "api_exec", claims.Username, claims.UserID, host.ID, "", req.Command, r.RemoteAddr)
	writeJSON(w, resp)
}

func (s *Server) webSSHConnect(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	q := r.URL.Query()
	q.Set("user_id", claims.UserID)
	q.Set("username", claims.Username)
	q.Set("tenant_id", claims.TenantID)
	if claims.IsRoot() {
		q.Set("root", "1")
	}
	r.URL.RawQuery = q.Encode()
	s.webSSH.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
