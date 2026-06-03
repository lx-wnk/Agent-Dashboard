package merger

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelDiscoveryExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, channelconfig.DiscoveryDir)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "4242.json"), []byte(`{"port":1}`), 0o600))

	assert.True(t, channelDiscoveryExists(4242), "discovery file present → channel available")
	assert.False(t, channelDiscoveryExists(9999), "no discovery file → not available")

	// A directory at the discovery path must not be mistaken for a channel.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "5555.json"), 0o700))
	assert.False(t, channelDiscoveryExists(5555), "directory at path → not available")
}
