// Package config loads and validates server configuration.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	"github.com/lx-wnk/agent-dashboard/server/internal/worktree"
)

// Config holds bootstrap and secret configuration. Operational config now lives
// in the DB-backed settings registry; these keys are the only ones still read
// from the environment. Keys match environment variable names after stripping
// the DASHBOARD_ prefix and lowercasing.
type Config struct {
	Host      string `koanf:"host"`
	Port      int    `koanf:"port"`
	JWTSecret string `koanf:"jwt_secret"`
	DBPath    string `koanf:"db_path"`
	PluginDir string `koanf:"plugin_dir"`
	// ProviderDir is an optional directory of user provider descriptors merged
	// over the built-ins. Set via DASHBOARD_PROVIDER_DIR.
	ProviderDir  string `koanf:"provider_dir"`
	WorktreeRoot string `koanf:"worktree_root"`
	HooksSecret  string `koanf:"hooks_secret"`
	MCPToken     string `koanf:"mcp_token"`
	// RemotesEnabled allows binding to a non-loopback address. Set via DASHBOARD_REMOTES_ENABLED=true.
	// Must be explicitly opted in because the dashboard exposes sensitive Claude session data.
	RemotesEnabled bool `koanf:"remotes_enabled"`
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
		Host:         "127.0.0.1",
		Port:         13120,
		DBPath:       home + "/.claude/dashboard-tasks.db",
		WorktreeRoot: home + "/" + worktree.DefaultRootDirName,
	}
}

// Load returns a Config merged from defaults → optional JSON file → env vars.
// Env vars are prefixed with DASHBOARD_ and case-insensitive.
func Load(cfgFile string) (Config, error) {
	k := koanf.New(".")
	cfg := Defaults()

	// Load defaults as base
	defaults := map[string]any{
		"host":          cfg.Host,
		"port":          cfg.Port,
		"db_path":       cfg.DBPath,
		"worktree_root": cfg.WorktreeRoot,
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

	warnOnMovedEnvKeys()

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

	// Refuse boot when binding to a non-loopback address unless the operator has
	// explicitly opted in via DASHBOARD_REMOTES_ENABLED=true. The dashboard reads
	// sensitive Claude session data; accidental public exposure is a high-impact mistake.
	loopback := map[string]bool{"127.0.0.1": true, "::1": true, "localhost": true}
	if !loopback[cfg.Host] {
		if !cfg.RemotesEnabled {
			return Config{}, fmt.Errorf(
				"config: DASHBOARD_HOST=%q is a non-loopback address and would expose sensitive Claude session data to the network. "+
					"Set DASHBOARD_REMOTES_ENABLED=true to confirm this is intentional (use a VPN or SSH tunnel), "+
					"or set DASHBOARD_HOST=127.0.0.1 to bind to loopback only",
				cfg.Host,
			)
		}
		slog.Warn("DASHBOARD_HOST is non-loopback — server will expose sensitive Claude session data to the network. Use VPN/SSH tunnel only.", "host", cfg.Host)
	}

	// Auto-generate and persist hooks secret when DASHBOARD_HOOKS_SECRET is not set.
	// This ensures /api/hooks/event is always protected, even on first boot.
	hooksSecret, err := loadOrGenerateHooksSecret(cfg.HooksSecret)
	if err != nil {
		return Config{}, err
	}
	cfg.HooksSecret = hooksSecret

	return cfg, nil
}

// IsLoopback reports whether the configured host is a loopback address.
func (c Config) IsLoopback() bool {
	return c.Host == "127.0.0.1" || c.Host == "::1" || c.Host == "localhost"
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

// warnOnMovedEnvKeys logs a warning for any DASHBOARD_ env var whose value moved
// to the DB-backed settings registry and is therefore no longer read here.
func warnOnMovedEnvKeys() {
	movedKeys := []string{
		"DASHBOARD_AUTH", "DASHBOARD_PROVIDERS_ENABLED", "DASHBOARD_ALLOW_GIT_PUSH",
		"DASHBOARD_ALLOW_GIT_PULL", "DASHBOARD_SPAWNER_ALLOWED_COMMANDS",
		"DASHBOARD_FORCE_WORKTREES", "DASHBOARD_SSE_INTERVAL_MS", "DASHBOARD_SHUTDOWN_TIMEOUT_SECONDS",
		"DASHBOARD_HOOKS_DEBOUNCE_MS", "DASHBOARD_HOOK_EVENTS_PER_SESSION",
		"DASHBOARD_SPAWN_RATE_LIMIT", "DASHBOARD_SPAWN_RATE_WINDOW_MS",
		"DASHBOARD_INJECT_RATE_LIMIT", "DASHBOARD_INJECT_RATE_WINDOW_MS",
		"DASHBOARD_COST_SCAN_INTERVAL_MS", "DASHBOARD_EVAL_SCAN_INTERVAL_MS",
		"DASHBOARD_EVAL_WINDOW_HOURS", "DASHBOARD_EVAL_MIN_SAMPLES",
		"DASHBOARD_EVAL_RATE_DROP_PP", "DASHBOARD_EVAL_STDDEV_K",
	}
	for _, key := range movedKeys {
		if _, ok := os.LookupEnv(key); ok {
			slog.Warn("config: env var is no longer read — manage it via the Settings UI or 'dashboard settings set'", "key", key)
		}
	}
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
