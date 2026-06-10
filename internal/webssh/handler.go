package webssh

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/neko233/ssh233-agent-server/internal/audit"
	"github.com/neko233/ssh233-agent-server/internal/bastion"
	"github.com/neko233/ssh233-agent-server/internal/models"
	"github.com/neko233/ssh233-agent-server/internal/store"
	"golang.org/x/crypto/ssh"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type Handler struct {
	store   *store.Store
	bastion *bastion.Server
	audit   *audit.Logger
	logger  *slog.Logger
}

func NewHandler(st *store.Store, b *bastion.Server, auditLog *audit.Logger) *Handler {
	return &Handler{
		store:   st,
		bastion: b,
		audit:   auditLog,
		logger:  slog.Default().With("component", "webssh"),
	}
}

type connectMsg struct {
	Type     string `json:"type"`
	HostID   string `json:"host_id"`
	Password string `json:"password,omitempty"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	username := r.URL.Query().Get("username")
	if userID == "" || username == "" {
		http.Error(w, "missing auth context", http.StatusUnauthorized)
		return
	}

	user, err := h.store.GetUserByID(userID)
	if err != nil || user == nil {
		http.Error(w, "invalid user", http.StatusUnauthorized)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	var msg connectMsg
	if err := ws.ReadJSON(&msg); err != nil {
		return
	}
	if msg.Type != "connect" || msg.HostID == "" {
		ws.WriteJSON(map[string]string{"type": "error", "message": "expected connect message with host_id"})
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	isRoot := r.URL.Query().Get("root") == "1"
	scope := store.Scope{TenantID: tenantID, Root: isRoot}

	host, err := h.store.GetHost(scope, msg.HostID)
	if err != nil || host == nil || !host.Enabled {
		ws.WriteJSON(map[string]string{"type": "error", "message": "host not found"})
		return
	}

	clientIP, _ := httpSplitHostPort(r.RemoteAddr)
	sess := &models.Session{
		TenantID:   host.TenantID,
		UserID:     user.ID,
		Username:   username,
		HostID:     host.ID,
		HostName:   host.Name,
		TargetAddr: fmt.Sprintf("%s@%s:%d", host.Username, host.Address, host.Port),
		Protocol:   "web",
		ClientIP:   clientIP,
	}
	_ = h.store.CreateSession(sess)

	var client *ssh.Client
	if msg.Password != "" {
		client, err = h.bastion.ConnectWithPassword(user, host, msg.Password)
	} else {
		client, err = h.bastion.ConnectWithKey(user, host)
	}
	if err != nil {
		ws.WriteJSON(map[string]string{"type": "error", "message": err.Error()})
		h.store.CloseSession(sess.ID)
		return
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		ws.WriteJSON(map[string]string{"type": "error", "message": err.Error()})
		return
	}
	defer session.Close()

	cols, rows := msg.Cols, msg.Rows
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 40
	}

	stdin, _ := session.StdinPipe()
	stdout, _ := session.StdoutPipe()
	stderr, _ := session.StderrPipe()

	session.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	})
	session.Shell()

	h.audit.Log(host.TenantID, "web_session_start", username, user.ID, host.ID, sess.ID, host.Name, clientIP)
	ws.WriteJSON(map[string]string{"type": "connected", "session_id": sess.ID})

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				ws.WriteJSON(map[string]string{"type": "data", "data": string(buf[:n])})
			}
			if err != nil {
				break
			}
		}
	}()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				ws.WriteJSON(map[string]string{"type": "data", "data": string(buf[:n])})
			}
			if err != nil {
				break
			}
		}
	}()

	go func() {
		for {
			var incoming map[string]string
			if err := ws.ReadJSON(&incoming); err != nil {
				stdin.Close()
				return
			}
			switch incoming["type"] {
			case "data":
				io.WriteString(stdin, incoming["data"])
			case "resize":
				// best-effort; window change via ssh channel not exposed here
			case "ping":
				ws.WriteJSON(map[string]string{"type": "pong"})
			}
		}
	}()

	<-done
	session.Wait()
	h.store.CloseSession(sess.ID)
	h.audit.Log(host.TenantID, "web_session_end", username, user.ID, host.ID, sess.ID, "", clientIP)
}

func httpSplitHostPort(addr string) (host, port string) {
	if i := len(addr) - 1; i >= 0 {
		if addr[0] == '[' {
			// ipv6
		}
	}
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i], addr[i+1:]
		}
	}
	return addr, ""
}
