// Package config loads and validates server configuration.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config holds all server configuration. Keys match environment variable
// names after stripping the DASHBOARD_ prefix and lowercasing.
type Config struct {
	Host                   string `koanf:"host"`
	Port                   int    `koanf:"port"`
	JWTSecret              string `koanf:"jwt_secret"`
	DBPath                 string `koanf:"db_path"`
	SSEIntervalMs          int    `koanf:"sse_interval_ms"`
	ShutdownTimeoutSeconds int    `koanf:"shutdown_timeout_seconds"`
	PluginDir              string `koanf:"plugin_dir"`
	AllowGitPush           bool   `koanf:"allow_git_push"`
	HooksSecret            string `koanf:"hooks_secret"`
	HooksDebounceMs        int    `koanf:"hooks_debounce_ms"`
	SpawnRateLimit         int    `koanf:"spawn_rate_limit"`
	SpawnRateWindowMs      int    `koanf:"spawn_rate_window_ms"`
	MCPToken     string `koanf:"mcp_token"`
	WorktreeRoot string `koanf:"worktree_root"`
	// Auth controls authentication mode.
	// "none" (default) — bypass auth, no login required.
	// "plugin" — require OAuth via an auth_provider plugin (GitHub, Office365, etc.).
	// "github" — deprecated alias for "plugin"; accepted for backwards compatibility.
	Auth string `koanf:"auth"`
	// AuthPluginSecret is the shared secret between core and auth plugins.
	// Set via DASHBOARD_AUTH_PLUGIN_SECRET. When set, enables POST /api/auth/session
	// so an external auth plugin can establish sessions after completing OAuth.
	AuthPluginSecret string        `koanf:"auth_plugin_secret"`
	Adapters         AdapterConfig `koanf:"adapters"`
}

// Defaults returns a Config populated with safe defaults.
func Defaults() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Host:                   "127.0.0.1",
		Port:                   13120,
		DBPath:                 home + "/.claude/dashboard-tasks.db",
		WorktreeRoot:           home + "/.claude/dashboard-worktrees",
		SSEIntervalMs:          3000,
		ShutdownTimeoutSeconds: 10,
		HooksDebounceMs:        100,
		SpawnRateLimit:         5,
		SpawnRateWindowMs:      60000,
		Auth:                   "none",
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
		"spawn_rate_limit":         cfg.SpawnRateLimit,
		"spawn_rate_window_ms":     cfg.SpawnRateWindowMs,
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

	// Validate auth mode. "github" is a deprecated alias for "plugin".
	switch cfg.Auth {
	case "none", "plugin":
		// valid
	case "github":
		slog.Warn("DASHBOARD_AUTH=github is deprecated — use DASHBOARD_AUTH=plugin instead")
		cfg.Auth = "plugin"
	default:
		return Config{}, fmt.Errorf("config: DASHBOARD_AUTH must be \"none\" or \"plugin\", got %q", cfg.Auth)
	}

	// Reject operator-set JWT secrets that are too short (< 32 chars).
	// The auto-generated secret is always 64 hex chars so this only fires for short manually-set values.
	if cfg.JWTSecret != "" && len(cfg.JWTSecret) < 32 {
		return Config{}, fmt.Errorf("config: DASHBOARD_JWT_SECRET must be at least 32 characters, got %d", len(cfg.JWTSecret))
	}

	// Reject auth plugin secrets that are too short — a short shared secret offers trivial brute-force surface.
	if cfg.AuthPluginSecret != "" && len(cfg.AuthPluginSecret) < 32 {
		return Config{}, fmt.Errorf("config: DASHBOARD_AUTH_PLUGIN_SECRET must be at least 32 characters, got %d", len(cfg.AuthPluginSecret))
	}

	if cfg.JWTSecret == "" {
		secret, err := randomHex(32)
		if err != nil {
			return Config{}, fmt.Errorf("config: generate jwt secret: %w", err)
		}
		cfg.JWTSecret = secret
		slog.Warn("DASHBOARD_JWT_SECRET not set — generated ephemeral secret; sessions will invalidate on restart")
	}

	// Warn when binding to a non-loopback address — dashboard reads sensitive session data.
	loopback := map[string]bool{"127.0.0.1": true, "::1": true, "localhost": true}
	if !loopback[cfg.Host] {
		slog.Warn("DASHBOARD_HOST is non-loopback — server will expose sensitive Claude session data to the network. Use VPN/SSH tunnel only.", "host", cfg.Host)
	}

	// Warn when hooks secret is unset — /api/hooks/event will accept unauthenticated requests.
	if cfg.HooksSecret == "" {
		slog.Warn("DASHBOARD_HOOKS_SECRET not set — /api/hooks/event is open to any loopback caller; set a secret when running in shared environments")
	}

	return cfg, nil
}

// IsLoopback reports whether the configured host is a loopback address.
func (c Config) IsLoopback() bool {
	return c.Host == "127.0.0.1" || c.Host == "::1" || c.Host == "localhost"
}

// ShutdownTimeout returns the graceful shutdown duration.
func (c Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.ShutdownTimeoutSeconds) * time.Second
}

// Addr returns the bind address string.
func (c Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// CallbackURL returns the OAuth redirect URI derived from Host and Port.
// Uses https for non-loopback hosts.
func (c Config) CallbackURL() string {
	scheme := "http"
	if !c.IsLoopback() {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/api/auth/callback", scheme, c.Addr())
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
