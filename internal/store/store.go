package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/neko233/ssh233-agent-server/internal/models"
	"golang.org/x/crypto/bcrypt"
)

func (s *Store) Bootstrap(adminUser, adminPassword string) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	defaultTenant := &models.Tenant{
		ID:      uuid.New().String(),
		Name:    "Default",
		Slug:    "default",
		Enabled: true,
	}
	if err := s.CreateTenant(defaultTenant); err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := time.Now()
	_, err = s.db.Exec(`INSERT INTO users (id, tenant_id, username, password_hash, role, enabled, created_at, updated_at)
		VALUES (?, '', ?, ?, ?, 1, ?, ?)`,
		uuid.New().String(), adminUser, string(hash), models.RoleRoot, now, now)
	return err
}

// --- Tenants ---

func (s *Store) CreateTenant(t *models.Tenant) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	now := time.Now()
	t.CreatedAt = now
	t.UpdatedAt = now
	_, err := s.db.Exec(`INSERT INTO tenants (id, name, slug, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Slug, boolToInt(t.Enabled), t.CreatedAt, t.UpdatedAt)
	return err
}

func (s *Store) GetTenantBySlug(slug string) (*models.Tenant, error) {
	t := &models.Tenant{}
	var enabled int
	err := s.db.QueryRow(`SELECT id, name, slug, enabled, created_at, updated_at FROM tenants WHERE slug = ?`, slug).
		Scan(&t.ID, &t.Name, &t.Slug, &enabled, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.Enabled = enabled == 1
	return t, nil
}

func (s *Store) GetTenant(id string) (*models.Tenant, error) {
	t := &models.Tenant{}
	var enabled int
	err := s.db.QueryRow(`SELECT id, name, slug, enabled, created_at, updated_at FROM tenants WHERE id = ?`, id).
		Scan(&t.ID, &t.Name, &t.Slug, &enabled, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.Enabled = enabled == 1
	return t, nil
}

func (s *Store) ListTenants() ([]models.Tenant, error) {
	rows, err := s.db.Query(`SELECT id, name, slug, enabled, created_at, updated_at FROM tenants ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tenants []models.Tenant
	for rows.Next() {
		var t models.Tenant
		var enabled int
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &enabled, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Enabled = enabled == 1
		tenants = append(tenants, t)
	}
	return tenants, nil
}

func (s *Store) UpdateTenant(t *models.Tenant) error {
	t.UpdatedAt = time.Now()
	_, err := s.db.Exec(`UPDATE tenants SET name=?, slug=?, enabled=?, updated_at=? WHERE id=?`,
		t.Name, t.Slug, boolToInt(t.Enabled), t.UpdatedAt, t.ID)
	return err
}

// --- Users ---

func scanUser(row scanner) (*models.User, error) {
	u := &models.User{}
	var enabled int
	err := row.Scan(&u.ID, &u.TenantID, &u.Username, &u.PasswordHash, &u.Role, &enabled, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.Enabled = enabled == 1
	return u, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func (s *Store) GetUserByUsername(tenantID, username string) (*models.User, error) {
	return scanUser(s.db.QueryRow(`SELECT id, tenant_id, username, password_hash, role, enabled, created_at, updated_at
		FROM users WHERE tenant_id = ? AND username = ?`, tenantID, username))
}

func (s *Store) GetRootUser(username string) (*models.User, error) {
	return scanUser(s.db.QueryRow(`SELECT id, tenant_id, username, password_hash, role, enabled, created_at, updated_at
		FROM users WHERE role = ? AND username = ?`, models.RoleRoot, username))
}

func (s *Store) GetUserByID(id string) (*models.User, error) {
	return scanUser(s.db.QueryRow(`SELECT id, tenant_id, username, password_hash, role, enabled, created_at, updated_at
		FROM users WHERE id = ?`, id))
}

func (s *Store) ListUsers(scope Scope) ([]models.User, error) {
	where, args := scope.tenantFilter("tenant_id")
	rows, err := s.db.Query(`SELECT id, tenant_id, username, role, enabled, created_at, updated_at FROM users WHERE `+where+` ORDER BY username`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []models.User
	for rows.Next() {
		var u models.User
		var enabled int
		if err := rows.Scan(&u.ID, &u.TenantID, &u.Username, &u.Role, &enabled, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		u.Enabled = enabled == 1
		users = append(users, u)
	}
	return users, nil
}

func (s *Store) CreateUser(u *models.User, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := time.Now()
	if u.ID == "" {
		u.ID = uuid.New().String()
	}
	u.PasswordHash = string(hash)
	u.CreatedAt = now
	u.UpdatedAt = now
	if u.Role == "" {
		u.Role = "operator"
	}
	_, err = s.db.Exec(`INSERT INTO users (id, tenant_id, username, password_hash, role, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
		u.ID, u.TenantID, u.Username, u.PasswordHash, u.Role, u.CreatedAt, u.UpdatedAt)
	return err
}

func (s *Store) VerifyPassword(u *models.User, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
}

// --- Hosts ---

func scanHost(row scanner) (*models.Host, error) {
	h := &models.Host{}
	var enabled int
	err := row.Scan(&h.ID, &h.TenantID, &h.Name, &h.Address, &h.Port, &h.Username, &h.Description, &h.Tags, &enabled, &h.CreatedAt, &h.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	h.Enabled = enabled == 1
	return h, nil
}

func (s *Store) CreateHost(h *models.Host) error {
	if h.ID == "" {
		h.ID = uuid.New().String()
	}
	now := time.Now()
	h.CreatedAt = now
	h.UpdatedAt = now
	if h.Port == 0 {
		h.Port = 22
	}
	_, err := s.db.Exec(`INSERT INTO hosts (id, tenant_id, name, address, port, username, description, tags, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.ID, h.TenantID, h.Name, h.Address, h.Port, h.Username, h.Description, h.Tags,
		boolToInt(h.Enabled), h.CreatedAt, h.UpdatedAt)
	return err
}

func (s *Store) UpdateHost(h *models.Host) error {
	h.UpdatedAt = time.Now()
	_, err := s.db.Exec(`UPDATE hosts SET name=?, address=?, port=?, username=?, description=?, tags=?, enabled=?, updated_at=?
		WHERE id=? AND tenant_id=?`,
		h.Name, h.Address, h.Port, h.Username, h.Description, h.Tags, boolToInt(h.Enabled), h.UpdatedAt, h.ID, h.TenantID)
	return err
}

func (s *Store) DeleteHost(scope Scope, id string) error {
	where, args := scope.tenantFilter("tenant_id")
	args = append(args, id)
	_, err := s.db.Exec(`DELETE FROM hosts WHERE `+where+` AND id = ?`, args...)
	return err
}

func (s *Store) GetHost(scope Scope, id string) (*models.Host, error) {
	where, args := scope.tenantFilter("tenant_id")
	args = append(args, id)
	return scanHost(s.db.QueryRow(`SELECT id, tenant_id, name, address, port, username, description, tags, enabled, created_at, updated_at
		FROM hosts WHERE `+where+` AND id = ?`, args...))
}

func (s *Store) GetHostByName(scope Scope, name string) (*models.Host, error) {
	where, args := scope.tenantFilter("tenant_id")
	args = append(args, name)
	return scanHost(s.db.QueryRow(`SELECT id, tenant_id, name, address, port, username, description, tags, enabled, created_at, updated_at
		FROM hosts WHERE `+where+` AND name = ?`, args...))
}

func (s *Store) ListHosts(scope Scope) ([]models.Host, error) {
	where, args := scope.tenantFilter("tenant_id")
	rows, err := s.db.Query(`SELECT id, tenant_id, name, address, port, username, description, tags, enabled, created_at, updated_at
		FROM hosts WHERE `+where+` ORDER BY name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hosts []models.Host
	for rows.Next() {
		var h models.Host
		var enabled int
		if err := rows.Scan(&h.ID, &h.TenantID, &h.Name, &h.Address, &h.Port, &h.Username, &h.Description, &h.Tags,
			&enabled, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, err
		}
		h.Enabled = enabled == 1
		hosts = append(hosts, h)
	}
	return hosts, nil
}

// --- Host Key Pairs ---

func (s *Store) GetHostKeyPair(userID, hostID string) (*models.HostKeyPair, error) {
	kp := &models.HostKeyPair{}
	var uploaded int
	err := s.db.QueryRow(`SELECT id, user_id, host_id, public_key, private_key, fingerprint, uploaded, created_at
		FROM host_key_pairs WHERE user_id = ? AND host_id = ?`, userID, hostID).Scan(
		&kp.ID, &kp.UserID, &kp.HostID, &kp.PublicKey, &kp.PrivateKey, &kp.Fingerprint, &uploaded, &kp.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	kp.Uploaded = uploaded == 1
	return kp, nil
}

func (s *Store) SaveHostKeyPair(kp *models.HostKeyPair) error {
	if kp.ID == "" {
		kp.ID = uuid.New().String()
	}
	kp.CreatedAt = time.Now()
	if s.driver == "mysql" {
		_, err := s.db.Exec(`INSERT INTO host_key_pairs (id, user_id, host_id, public_key, private_key, fingerprint, uploaded, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE public_key=VALUES(public_key), private_key=VALUES(private_key),
			fingerprint=VALUES(fingerprint), uploaded=VALUES(uploaded)`,
			kp.ID, kp.UserID, kp.HostID, kp.PublicKey, kp.PrivateKey, kp.Fingerprint, boolToInt(kp.Uploaded), kp.CreatedAt)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO host_key_pairs (id, user_id, host_id, public_key, private_key, fingerprint, uploaded, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, host_id) DO UPDATE SET public_key=excluded.public_key, private_key=excluded.private_key,
		fingerprint=excluded.fingerprint, uploaded=excluded.uploaded`,
		kp.ID, kp.UserID, kp.HostID, kp.PublicKey, kp.PrivateKey, kp.Fingerprint, boolToInt(kp.Uploaded), kp.CreatedAt)
	return err
}

// --- Agents ---

func (s *Store) RegisterAgent(tenantID, name, token, hostname, ip, version string, capabilities []string) (*models.Agent, error) {
	capJSON, _ := json.Marshal(capabilities)
	now := time.Now()
	a := &models.Agent{
		ID:        uuid.New().String(),
		TenantID:  tenantID,
		Name:      name,
		Token:     token,
		Hostname:  hostname,
		IP:        ip,
		Version:   version,
		Status:    "online",
		LastSeen:  now,
		CreatedAt: now,
	}
	_, err := s.db.Exec(`INSERT INTO agents (id, tenant_id, name, token, hostname, ip, version, status, last_seen, capabilities, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'online', ?, ?, ?)`,
		a.ID, a.TenantID, a.Name, a.Token, a.Hostname, a.IP, a.Version, a.LastSeen, string(capJSON), a.CreatedAt)
	return a, err
}

func (s *Store) GetAgentByToken(token string) (*models.Agent, error) {
	a := &models.Agent{}
	var capJSON string
	err := s.db.QueryRow(`SELECT id, tenant_id, name, token, hostname, ip, version, status, last_seen, capabilities, created_at
		FROM agents WHERE token = ?`, token).Scan(
		&a.ID, &a.TenantID, &a.Name, &a.Token, &a.Hostname, &a.IP, &a.Version, &a.Status, &a.LastSeen, &capJSON, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(capJSON), &a.Capabilities)
	return a, nil
}

func (s *Store) AgentHeartbeat(id, ip string) error {
	_, err := s.db.Exec(`UPDATE agents SET status='online', last_seen=?, ip=? WHERE id=?`, time.Now(), ip, id)
	return err
}

func (s *Store) ListAgents(scope Scope) ([]models.Agent, error) {
	where, args := scope.tenantFilter("tenant_id")
	rows, err := s.db.Query(`SELECT id, tenant_id, name, hostname, ip, version, status, last_seen, created_at
		FROM agents WHERE `+where+` ORDER BY name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var agents []models.Agent
	for rows.Next() {
		var a models.Agent
		if err := rows.Scan(&a.ID, &a.TenantID, &a.Name, &a.Hostname, &a.IP, &a.Version, &a.Status, &a.LastSeen, &a.CreatedAt); err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, nil
}

func (s *Store) MarkStaleAgentsOffline(cutoff time.Time) error {
	_, err := s.db.Exec(`UPDATE agents SET status='offline' WHERE last_seen < ? AND status='online'`, cutoff)
	return err
}

// --- Sessions ---

func (s *Store) CreateSession(sess *models.Session) error {
	if sess.ID == "" {
		sess.ID = uuid.New().String()
	}
	sess.StartedAt = time.Now()
	sess.Status = "active"
	_, err := s.db.Exec(`INSERT INTO sessions (id, tenant_id, user_id, username, host_id, host_name, target_addr, protocol, client_ip, status, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.TenantID, nullStr(sess.UserID), sess.Username, nullStr(sess.HostID), sess.HostName,
		sess.TargetAddr, sess.Protocol, sess.ClientIP, sess.Status, sess.StartedAt)
	return err
}

func (s *Store) CloseSession(id string) error {
	_, err := s.db.Exec(`UPDATE sessions SET status='closed', ended_at=? WHERE id=?`, time.Now(), id)
	return err
}

func (s *Store) ListSessions(scope Scope, limit int) ([]models.Session, error) {
	if limit <= 0 {
		limit = 50
	}
	where, args := scope.tenantFilter("tenant_id")
	args = append(args, limit)
	rows, err := s.db.Query(`SELECT id, tenant_id, user_id, username, host_id, host_name, target_addr, protocol, client_ip, status, started_at, ended_at
		FROM sessions WHERE `+where+` ORDER BY started_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessions(rows)
}

func (s *Store) GetActiveSessions(scope Scope) ([]models.Session, error) {
	where, args := scope.tenantFilter("tenant_id")
	rows, err := s.db.Query(`SELECT id, tenant_id, user_id, username, host_id, host_name, target_addr, protocol, client_ip, status, started_at, ended_at
		FROM sessions WHERE `+where+` AND status='active' ORDER BY started_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessions(rows)
}

func scanSessions(rows *sql.Rows) ([]models.Session, error) {
	var sessions []models.Session
	for rows.Next() {
		var sess models.Session
		var userID, hostID sql.NullString
		var endedAt sql.NullTime
		if err := rows.Scan(&sess.ID, &sess.TenantID, &userID, &sess.Username, &hostID, &sess.HostName,
			&sess.TargetAddr, &sess.Protocol, &sess.ClientIP, &sess.Status, &sess.StartedAt, &endedAt); err != nil {
			return nil, err
		}
		if userID.Valid {
			sess.UserID = userID.String
		}
		if hostID.Valid {
			sess.HostID = hostID.String
		}
		if endedAt.Valid {
			sess.EndedAt = &endedAt.Time
		}
		sessions = append(sessions, sess)
	}
	return sessions, nil
}

// --- Audit ---

func (s *Store) WriteAudit(log *models.AuditLog) error {
	if log.ID == "" {
		log.ID = uuid.New().String()
	}
	log.CreatedAt = time.Now()
	_, err := s.db.Exec(`INSERT INTO audit_logs (id, tenant_id, session_id, user_id, username, host_id, action, detail, client_ip, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.ID, log.TenantID, log.SessionID, log.UserID, log.Username, log.HostID, log.Action, log.Detail, log.ClientIP, log.CreatedAt)
	return err
}

func (s *Store) ListAuditLogs(scope Scope, limit, offset int) ([]models.AuditLog, error) {
	if limit <= 0 {
		limit = 100
	}
	where, args := scope.tenantFilter("tenant_id")
	args = append(args, limit, offset)
	rows, err := s.db.Query(`SELECT id, tenant_id, session_id, user_id, username, host_id, action, detail, client_ip, created_at
		FROM audit_logs WHERE `+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var logs []models.AuditLog
	for rows.Next() {
		var l models.AuditLog
		if err := rows.Scan(&l.ID, &l.TenantID, &l.SessionID, &l.UserID, &l.Username, &l.HostID,
			&l.Action, &l.Detail, &l.ClientIP, &l.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, nil
}

func (s *Store) RecordCommand(rec *models.CommandRecord) error {
	if rec.ID == "" {
		rec.ID = uuid.New().String()
	}
	rec.CreatedAt = time.Now()
	_, err := s.db.Exec(`INSERT INTO command_records (id, session_id, command, output, exit_code, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.SessionID, rec.Command, rec.Output, rec.ExitCode, rec.CreatedAt)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullStr(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}

// EnsureAdmin is kept for compatibility in tests.
func (s *Store) EnsureAdmin(username, password string) error {
	return s.Bootstrap(username, password)
}
