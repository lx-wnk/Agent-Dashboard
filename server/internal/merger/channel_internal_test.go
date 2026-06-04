package merger

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelDiscovery(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, channelconfig.DiscoveryDir)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "4242.json"), []byte(`{"port":1}`), 0o600))

	avail, inject := channelDiscovery(4242)
	assert.True(t, avail, "discovery file present → channel available")
	assert.False(t, inject, "no tmuxPane/ptyInject → not live-injectable")

	avail, _ = channelDiscovery(9999)
	assert.False(t, avail, "no discovery file → not available")

	// A discovery file carrying a tmux pane reference is live-injectable.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "7777.json"), []byte(`{"tmuxPane":"%3"}`), 0o600))
	avail, inject = channelDiscovery(7777)
	assert.True(t, avail, "discovery file present → channel available")
	assert.True(t, inject, "tmuxPane set → live-injectable")

	// A directory at the discovery path must not be mistaken for a channel.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "5555.json"), 0o700))
	avail, _ = channelDiscovery(5555)
	assert.False(t, avail, "directory at path → not available")
}
