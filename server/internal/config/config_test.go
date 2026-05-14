package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_ValidDefaultConfig(t *testing.T) {
	// No env vars set — should load with defaults and auto-generate JWT secret.
	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", cfg.Host)
	assert.Equal(t, 13120, cfg.Port)
	assert.Equal(t, 5, cfg.SpawnRateLimit)
	assert.Equal(t, 60000, cfg.SpawnRateWindowMs)
	assert.Equal(t, 3000, cfg.SSEIntervalMs)
	assert.Equal(t, 100, cfg.HooksDebounceMs)
	// Auto-generated JWT secret must be 64 hex chars (32 bytes).
	assert.Len(t, cfg.JWTSecret, 64)
}

func TestLoad_DefaultsAppliedWhenEnvAbsent(t *testing.T) {
	defaults := Defaults()
	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, defaults.Host, cfg.Host)
	assert.Equal(t, defaults.Port, cfg.Port)
	assert.Equal(t, defaults.SSEIntervalMs, cfg.SSEIntervalMs)
	assert.Equal(t, defaults.ShutdownTimeoutSeconds, cfg.ShutdownTimeoutSeconds)
	assert.Equal(t, defaults.HooksDebounceMs, cfg.HooksDebounceMs)
	assert.Equal(t, defaults.SpawnRateLimit, cfg.SpawnRateLimit)
	assert.Equal(t, defaults.SpawnRateWindowMs, cfg.SpawnRateWindowMs)
}

func TestLoad_CustomValuesFromEnv(t *testing.T) {
	t.Setenv("DASHBOARD_HOST", "localhost")
	t.Setenv("DASHBOARD_PORT", "9090")
	t.Setenv("DASHBOARD_SSE_INTERVAL_MS", "5000")
	t.Setenv("DASHBOARD_SPAWN_RATE_LIMIT", "10")
	t.Setenv("DASHBOARD_SPAWN_RATE_WINDOW_MS", "30000")

	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, "localhost", cfg.Host)
	assert.Equal(t, 9090, cfg.Port)
	assert.Equal(t, 5000, cfg.SSEIntervalMs)
	assert.Equal(t, 10, cfg.SpawnRateLimit)
	assert.Equal(t, 30000, cfg.SpawnRateWindowMs)
}

func TestLoad_JWTSecretTooShort(t *testing.T) {
	t.Setenv("DASHBOARD_JWT_SECRET", "tooshort")

	_, err := Load("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DASHBOARD_JWT_SECRET must be at least 32 characters")
}

func TestLoad_JWTSecretExactly32Chars(t *testing.T) {
	secret := "abcdefghijklmnopqrstuvwxyz123456" // exactly 32 chars
	t.Setenv("DASHBOARD_JWT_SECRET", secret)

	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, secret, cfg.JWTSecret)
}

func TestLoad_JWTSecretLongerThan32Chars(t *testing.T) {
	secret := "abcdefghijklmnopqrstuvwxyz1234567890abcdef" // > 32 chars
	t.Setenv("DASHBOARD_JWT_SECRET", secret)

	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, secret, cfg.JWTSecret)
}

func TestLoad_IsLoopback(t *testing.T) {
	cfg, err := Load("")
	require.NoError(t, err)
	assert.True(t, cfg.IsLoopback())
}

func TestLoad_Addr(t *testing.T) {
	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:13120", cfg.Addr())
}
