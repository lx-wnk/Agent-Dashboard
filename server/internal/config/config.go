// Package config loads and validates server configuration.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/v2"
)

// Config holds all server configuration. Keys match environment variable
// names after stripping the DASHBOARD_ prefix and lowercasing.
type Config struct {
	Host                   string `koanf:"host"`
	Port                   int    `koanf:"port"`
	JWTSecret              string `koanf:"jwt_secret"`
	GitHubClientID         string `koanf:"github_client_id"`
	GitHubClientSecret     string `koanf:"github_client_secret"`
	DBPath                 string `koanf:"db_path"`
	SSEIntervalMs          int    `koanf:"sse_interval_ms"`
	ShutdownTimeoutSeconds int    `koanf:"shutdown_timeout_seconds"`
	PluginDir              string `koanf:"plugin_dir"`
	AllowGitPush           bool   `koanf:"allow_git_push"`
	HooksSecret            string `koanf:"hooks_secret"`
	HooksDebounceMs        int    `koanf:"hooks_debounce_ms"`
}

// Defaults returns a Config populated with safe defaults.
func Defaults() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Host:                   "127.0.0.1",
		Port:                   13120,
		DBPath:                 home + "/.claude/dashboard-tasks.db",
		SSEIntervalMs:          3000,
		ShutdownTimeoutSeconds: 10,
		HooksDebounceMs:        100,
	}
}

// Load returns a Config merged from defaults → optional JSON file → env vars.
// Env vars are prefixed with DASHBOARD_ and case-insensitive.
func Load(cfgFile string) (Config, error) {
	k := koanf.New(".")
	cfg := Defaults()

	// Load defaults as base
	defaults := map[string]any{
		"host":                     cfg.Host,
		"port":                     cfg.Port,
		"db_path":                  cfg.DBPath,
		"sse_interval_ms":          cfg.SSEIntervalMs,
		"shutdown_timeout_seconds": cfg.ShutdownTimeoutSeconds,
		"hooks_debounce_ms":        cfg.HooksDebounceMs,
	}
	if err := k.Load(confmap.Provider(defaults, "."), nil); err != nil {
		return Config{}, fmt.Errorf("config defaults: %w", err)
	}

	// Optional file override
	if cfgFile != "" {
		if err := k.Load(file.Provider(cfgFile), json.Parser()); err != nil {
			return Config{}, fmt.Errorf("config file %s: %w", cfgFile, err)
		}
	}

	// Env vars: DASHBOARD_HOST → host, DASHBOARD_JWT_SECRET → jwt_secret
	if err := k.Load(env.Provider("DASHBOARD_", ".", func(s string) string {
		return strings.ToLower(strings.TrimPrefix(s, "DASHBOARD_"))
	}), nil); err != nil {
		return Config{}, fmt.Errorf("config env: %w", err)
	}

	if err := k.Unmarshal("", &cfg); err != nil {
		return Config{}, fmt.Errorf("config unmarshal: %w", err)
	}
	return cfg, nil
}

// ShutdownTimeout returns the graceful shutdown duration.
func (c Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.ShutdownTimeoutSeconds) * time.Second
}

// Addr returns the bind address string.
func (c Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
