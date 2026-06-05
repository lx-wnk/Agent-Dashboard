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

// TestChannelDiscovery_TwoFileModel verifies the two-file model introduced to
// fix the pty-broker vs channel-bridge discovery collision on the no-tmux path.
func TestChannelDiscovery_TwoFileModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, channelconfig.DiscoveryDir)
	require.NoError(t, os.MkdirAll(dir, 0o700))

	t.Run("neither file → not available", func(t *testing.T) {
		avail, inject := channelDiscovery(1001)
		assert.False(t, avail)
		assert.False(t, inject)
	})

	t.Run("only pty file with ptyInject → available and injectable", func(t *testing.T) {
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "1002.pty.json"),
			[]byte(`{"port":9000,"token":"t","ptyInject":true}`),
			0o600,
		))
		avail, inject := channelDiscovery(1002)
		assert.True(t, avail, "pty file present → channel available")
		assert.True(t, inject, "ptyInject:true in pty file → live-injectable")
	})

	t.Run("only bridge file no tmux → available but not injectable", func(t *testing.T) {
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "1003.json"),
			[]byte(`{"port":9001,"token":"t"}`),
			0o600,
		))
		avail, inject := channelDiscovery(1003)
		assert.True(t, avail, "bridge file present → channel available")
		assert.False(t, inject, "no tmuxPane and no pty file → not injectable")
	})

	t.Run("only bridge file with tmuxPane → available and injectable", func(t *testing.T) {
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "1004.json"),
			[]byte(`{"port":9002,"token":"t","tmuxPane":"%3"}`),
			0o600,
		))
		avail, inject := channelDiscovery(1004)
		assert.True(t, avail)
		assert.True(t, inject, "tmuxPane in bridge file → live-injectable")
	})

	t.Run("both files → available and injectable", func(t *testing.T) {
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "1005.json"),
			[]byte(`{"port":9003,"token":"t"}`),
			0o600,
		))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "1005.pty.json"),
			[]byte(`{"port":9004,"token":"t2","ptyInject":true}`),
			0o600,
		))
		avail, inject := channelDiscovery(1005)
		assert.True(t, avail, "bridge+pty file → channel available")
		assert.True(t, inject, "pty file with ptyInject → live-injectable")
	})
}
