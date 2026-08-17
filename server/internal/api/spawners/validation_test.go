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
