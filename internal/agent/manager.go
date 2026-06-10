package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/neko233/ssh233-agent-server/internal/audit"
	"github.com/neko233/ssh233-agent-server/internal/config"
	"github.com/neko233/ssh233-agent-server/internal/models"
	"github.com/neko233/ssh233-agent-server/internal/store"
	"golang.org/x/crypto/ssh"
)

type Manager struct {
	cfg    *config.Config
	store  *store.Store
	audit  *audit.Logger
	logger *slog.Logger
	mu     sync.RWMutex
}

func NewManager(cfg *config.Config, st *store.Store, auditLog *audit.Logger) *Manager {
	return &Manager{
		cfg:    cfg,
		store:  st,
		audit:  auditLog,
		logger: slog.Default().With("component", "agent"),
	}
}

type RegisterRequest struct {
	Name          string   `json:"name"`
	RegisterToken string   `json:"register_token"`
	TenantSlug    string   `json:"tenant_slug"`
	Hostname      string   `json:"hostname"`
	IP            string   `json:"ip"`
	Version       string   `json:"version"`
	Capabilities  []string `json:"capabilities"`
}

type RegisterResponse struct {
	AgentID string `json:"agent_id"`
	Token   string `json:"token"`
}

func (m *Manager) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	if req.RegisterToken != m.cfg.Agent.RegisterToken {
		http.Error(w, `{"error":"invalid register token"}`, http.StatusUnauthorized)
		return
	}
	if req.TenantSlug == "" {
		req.TenantSlug = "default"
	}
	tenant, err := m.store.GetTenantBySlug(req.TenantSlug)
	if err != nil || tenant == nil {
		http.Error(w, `{"error":"unknown tenant"}`, http.StatusBadRequest)
		return
	}
	token := uuid.New().String()
	agent, err := m.store.RegisterAgent(tenant.ID, req.Name, token, req.Hostname, req.IP, req.Version, req.Capabilities)
	if err != nil {
		http.Error(w, `{"error":"register failed"}`, http.StatusInternalServerError)
		return
	}
	m.audit.Log(tenant.ID, "agent_register", "system", "", "", "", "agent="+req.Name, r.RemoteAddr)
	writeJSON(w, RegisterResponse{AgentID: agent.ID, Token: token})
}

type HeartbeatRequest struct {
	Token string `json:"token"`
	IP    string `json:"ip"`
}

func (m *Manager) Heartbeat(w http.ResponseWriter, r *http.Request) {
	var req HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	agent, err := m.store.GetAgentByToken(req.Token)
	if err != nil || agent == nil {
		http.Error(w, `{"error":"unknown agent"}`, http.StatusUnauthorized)
		return
	}
	_ = m.store.AgentHeartbeat(agent.ID, req.IP)
	writeJSON(w, map[string]string{"status": "ok"})
}

type ExecResponse struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exit_code"`
}

func (m *Manager) ExecViaSSH(user *models.User, host *models.Host, command, password string, dial func(*models.User, *models.Host, string) (*ssh.Client, error)) (*ExecResponse, error) {
	var client *ssh.Client
	var err error
	if password != "" {
		client, err = dial(user, host, password)
	} else {
		client, err = dial(user, host, "")
	}
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer session.Close()

	out, err := session.CombinedOutput(command)
	resp := &ExecResponse{Output: string(out)}
	if err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			resp.ExitCode = exitErr.ExitStatus()
			return resp, nil
		}
		return nil, err
	}
	return resp, nil
}

func (m *Manager) StartStaleChecker(stop <-chan struct{}) {
	ticker := time.NewTicker(m.cfg.Agent.HeartbeatTTL.Duration())
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-m.cfg.Agent.HeartbeatTTL.Duration() * 2)
			_ = m.store.MarkStaleAgentsOffline(cutoff)
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
