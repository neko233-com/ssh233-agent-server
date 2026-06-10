package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/neko233/ssh233-agent-server/internal/config"
	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

type Store struct {
	db     *sql.DB
	driver string
}

func Open(cfg *config.DatabaseConfig) (*Store, error) {
	driver := cfg.Driver
	if driver == "" {
		driver = "sqlite"
	}
	switch driver {
	case "sqlite":
		path := cfg.SQLite.Path
		if path == "" {
			path = "data/ssh233.db"
		}
		if path != ":memory:" {
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return nil, err
			}
		}
		db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
		if err != nil {
			return nil, err
		}
		s := &Store{db: db, driver: "sqlite"}
		if err := s.migrate(); err != nil {
			db.Close()
			return nil, err
		}
		return s, nil
	case "mysql":
		if cfg.MySQL.DSN == "" {
			return nil, fmt.Errorf("mysql dsn is required")
		}
		db, err := sql.Open("mysql", cfg.MySQL.DSN)
		if err != nil {
			return nil, err
		}
		if err := db.Ping(); err != nil {
			db.Close()
			return nil, fmt.Errorf("mysql ping: %w", err)
		}
		s := &Store{db: db, driver: "mysql"}
		if err := s.migrate(); err != nil {
			db.Close()
			return nil, err
		}
		return s, nil
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", driver)
	}
}

// OpenSQLite opens an in-memory or file sqlite store (for tests).
func OpenSQLite(path string) (*Store, error) {
	return Open(&config.DatabaseConfig{
		Driver: "sqlite",
		SQLite: config.SQLiteConfig{Path: path},
	})
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Driver() string {
	return s.driver
}

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tenants (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			slug VARCHAR(64) NOT NULL UNIQUE,
			enabled TINYINT NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id VARCHAR(36) PRIMARY KEY,
			tenant_id VARCHAR(36) NOT NULL DEFAULT '',
			username VARCHAR(128) NOT NULL,
			password_hash TEXT NOT NULL,
			role VARCHAR(32) NOT NULL DEFAULT 'operator',
			enabled TINYINT NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			UNIQUE (tenant_id, username)
		)`,
		`CREATE TABLE IF NOT EXISTS hosts (
			id VARCHAR(36) PRIMARY KEY,
			tenant_id VARCHAR(36) NOT NULL,
			name VARCHAR(128) NOT NULL,
			address VARCHAR(255) NOT NULL,
			port INT NOT NULL DEFAULT 22,
			username VARCHAR(128) NOT NULL DEFAULT 'root',
			description TEXT,
			tags VARCHAR(255) DEFAULT '',
			enabled TINYINT NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			UNIQUE (tenant_id, name)
		)`,
		`CREATE TABLE IF NOT EXISTS host_key_pairs (
			id VARCHAR(36) PRIMARY KEY,
			user_id VARCHAR(36) NOT NULL,
			host_id VARCHAR(36) NOT NULL,
			public_key TEXT NOT NULL,
			private_key TEXT NOT NULL,
			fingerprint VARCHAR(128) NOT NULL,
			uploaded TINYINT NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			UNIQUE (user_id, host_id)
		)`,
		`CREATE TABLE IF NOT EXISTS agents (
			id VARCHAR(36) PRIMARY KEY,
			tenant_id VARCHAR(36) NOT NULL,
			name VARCHAR(128) NOT NULL,
			token VARCHAR(64) NOT NULL UNIQUE,
			hostname VARCHAR(255) DEFAULT '',
			ip VARCHAR(64) DEFAULT '',
			version VARCHAR(64) DEFAULT '',
			status VARCHAR(32) NOT NULL DEFAULT 'offline',
			last_seen DATETIME NULL,
			capabilities TEXT DEFAULT '[]',
			created_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id VARCHAR(36) PRIMARY KEY,
			tenant_id VARCHAR(36) NOT NULL DEFAULT '',
			user_id VARCHAR(36),
			username VARCHAR(128) NOT NULL,
			host_id VARCHAR(36),
			host_name VARCHAR(128) DEFAULT '',
			target_addr VARCHAR(255) NOT NULL,
			protocol VARCHAR(32) NOT NULL,
			client_ip VARCHAR(64) DEFAULT '',
			status VARCHAR(32) NOT NULL DEFAULT 'active',
			started_at DATETIME NOT NULL,
			ended_at DATETIME NULL
		)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id VARCHAR(36) PRIMARY KEY,
			tenant_id VARCHAR(36) NOT NULL DEFAULT '',
			session_id VARCHAR(36) DEFAULT '',
			user_id VARCHAR(36) DEFAULT '',
			username VARCHAR(128) NOT NULL,
			host_id VARCHAR(36) DEFAULT '',
			action VARCHAR(64) NOT NULL,
			detail TEXT,
			client_ip VARCHAR(64) DEFAULT '',
			created_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS command_records (
			id VARCHAR(36) PRIMARY KEY,
			session_id VARCHAR(36) NOT NULL,
			command TEXT NOT NULL,
			output TEXT,
			exit_code INT NULL,
			created_at DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_created ON audit_logs(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(status)`,
		`CREATE INDEX IF NOT EXISTS idx_agents_status ON agents(status)`,
		`CREATE INDEX IF NOT EXISTS idx_hosts_tenant ON hosts(tenant_id)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}
