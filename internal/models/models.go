package models

import "time"

const RoleRoot = "root"

type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type User struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"` // root, admin, operator, viewer
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (u *User) IsRoot() bool {
	return u.Role == RoleRoot
}

type Host struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Name        string    `json:"name"`
	Address     string    `json:"address"`
	Port        int       `json:"port"`
	Username    string    `json:"username"`
	Description string    `json:"description"`
	Tags        string    `json:"tags"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type HostKeyPair struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	HostID      string    `json:"host_id"`
	PublicKey   string    `json:"public_key"`
	PrivateKey  string    `json:"-"`
	Fingerprint string    `json:"fingerprint"`
	Uploaded    bool      `json:"uploaded"`
	CreatedAt   time.Time `json:"created_at"`
}

type Agent struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	Name         string    `json:"name"`
	Token        string    `json:"token,omitempty"`
	Hostname     string    `json:"hostname"`
	IP           string    `json:"ip"`
	Version      string    `json:"version"`
	Status       string    `json:"status"`
	LastSeen     time.Time `json:"last_seen"`
	Capabilities []string  `json:"capabilities"`
	CreatedAt    time.Time `json:"created_at"`
}

type Session struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"tenant_id"`
	UserID     string     `json:"user_id"`
	Username   string     `json:"username"`
	HostID     string     `json:"host_id"`
	HostName   string     `json:"host_name"`
	TargetAddr string     `json:"target_addr"`
	Protocol   string     `json:"protocol"`
	ClientIP   string     `json:"client_ip"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
}

type AuditLog struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	HostID    string    `json:"host_id"`
	Action    string    `json:"action"`
	Detail    string    `json:"detail"`
	ClientIP  string    `json:"client_ip"`
	CreatedAt time.Time `json:"created_at"`
}

type CommandRecord struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Command   string    `json:"command"`
	Output    string    `json:"output,omitempty"`
	ExitCode  *int      `json:"exit_code,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
