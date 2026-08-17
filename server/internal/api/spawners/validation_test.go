package spawners

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateAdapterType_AcceptsAcp(t *testing.T) {
	msg, ok := ValidateAdapterType("acp")
	require.True(t, ok)
	require.Empty(t, msg)
}

func TestValidateAdapterConfig_AcpAcceptsCommandAndArgs(t *testing.T) {
	msg, ok := ValidateAdapterConfig("acp", map[string]string{
		"command": "npx",
		"args":    "-y @agentclientprotocol/claude-agent-acp@0.68.0",
	})
	require.True(t, ok)
	require.Empty(t, msg)
}

func TestValidateAdapterConfig_AcpRejectsUnknownKey(t *testing.T) {
	msg, ok := ValidateAdapterConfig("acp", map[string]string{"host": "localhost"})
	require.False(t, ok)
	require.Equal(t, "unknown adapter_config key: host", msg)
}

func TestValidateAdapterConfig_AcpRejectsUntrustedCommand(t *testing.T) {
	msg, ok := ValidateAdapterConfig("acp", map[string]string{"command": "/tmp/evil"})
	require.False(t, ok)
	require.Contains(t, msg, "could not be resolved")
}

func TestValidateAdapterConfig_AcpRejectsUnknownBareCommand(t *testing.T) {
	msg, ok := ValidateAdapterConfig("acp", map[string]string{"command": "evil-binary"})
	require.False(t, ok)
	require.Contains(t, msg, "not in the allow-list")
}

func TestValidateAdapterConfig_AcpAcceptsAllowlistedBareCommand(t *testing.T) {
	msg, ok := ValidateAdapterConfig("acp", map[string]string{"command": "npx"})
	require.True(t, ok)
	require.Empty(t, msg)
}

// TestValidateAdapterConfig_AcpAcceptsAbsoluteTrustedCommand covers the
// absolute-path accept branch: a path that resolves and whose parent is a
// trusted bin dir. /bin/sh exists on both macOS and Linux.
func TestValidateAdapterConfig_AcpAcceptsAbsoluteTrustedCommand(t *testing.T) {
	msg, ok := ValidateAdapterConfig("acp", map[string]string{"command": "/bin/sh"})
	require.True(t, ok)
	require.Empty(t, msg)
}

func TestValidateAdapterConfig_AcpAcceptsEmptyCommand(t *testing.T) {
	msg, ok := ValidateAdapterConfig("acp", map[string]string{"command": ""})
	require.True(t, ok)
	require.Empty(t, msg)
}

func TestValidateAdapterConfig_OllamaUnaffectedByCommandCheck(t *testing.T) {
	msg, ok := ValidateAdapterConfig("ollama", map[string]string{"host": "http://localhost:11434"})
	require.True(t, ok)
	require.Empty(t, msg)
}
