package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	SSH      SSHConfig      `yaml:"ssh"`
	Agent    AgentConfig    `yaml:"agent"`
}

type ServerConfig struct {
	HTTPAddr string `yaml:"http_addr"`
	SSHAddr  string `yaml:"ssh_addr"`
}

type DatabaseConfig struct {
	Driver string       `yaml:"driver"` // sqlite | mysql
	SQLite SQLiteConfig `yaml:"sqlite"`
	MySQL  MySQLConfig  `yaml:"mysql"`
}

type SQLiteConfig struct {
	Path string `yaml:"path"`
}

type MySQLConfig struct {
	DSN string `yaml:"dsn"`
}

type AuthConfig struct {
	JWTSecret     string   `yaml:"jwt_secret"`
	TokenTTL      Duration `yaml:"token_ttl"`
	AdminUser     string   `yaml:"admin_user"`
	AdminPassword string   `yaml:"admin_password"`
}

type SSHConfig struct {
	HostKeyPath string `yaml:"host_key_path"`
	Banner      string `yaml:"banner"`
}

type AgentConfig struct {
	RegisterToken string   `yaml:"register_token"`
	HeartbeatTTL  Duration `yaml:"heartbeat_ttl"`
}

type Duration time.Duration

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

func Default() *Config {
	return &Config{
		Server: ServerConfig{
			HTTPAddr: ":6030",
			SSHAddr:  ":2222",
		},
		Database: DatabaseConfig{
			Driver: "sqlite",
			SQLite: SQLiteConfig{Path: "data/ssh233.db"},
		},
		Auth: AuthConfig{
			JWTSecret:     "change-me-in-production",
			TokenTTL:      Duration(24 * time.Hour),
			AdminUser:     "root",
			AdminPassword: "root",
		},
		SSH: SSHConfig{
			HostKeyPath: "data/host_key",
			Banner:      "SSH233 Agent Server - Authorized access only\n",
		},
		Agent: AgentConfig{
			RegisterToken: "agent-register-token",
			HeartbeatTTL:  Duration(60 * time.Second),
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Database.Driver == "" {
		cfg.Database.Driver = "sqlite"
	}
	return cfg, nil
}

func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func (c *DatabaseConfig) Dialect() string {
	if c.Driver == "mysql" {
		return "mysql"
	}
	return "sqlite"
}
