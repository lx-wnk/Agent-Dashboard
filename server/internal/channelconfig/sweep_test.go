package channelconfig

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSweepOrphanedConfigs_RemovesOnlyOldMatchingFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	write := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte("{}"), 0o600))
		require.NoError(t, os.Chtimes(p, now.Add(-age), now.Add(-age)))
		return p
	}
	oldCfg := write("dashboard-channel-mcp-old.json", 48*time.Hour)
	freshCfg := write("dashboard-channel-mcp-fresh.json", time.Minute)
	unrelated := write("something-else.json", 48*time.Hour)

	n, err := sweepOrphanedConfigsIn(dir, OrphanedConfigMaxAge, now)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.NoFileExists(t, oldCfg)
	require.FileExists(t, freshCfg, "another instance's live run may still need it")
	require.FileExists(t, unrelated)
}

func TestSweepOrphanedConfigs_MissingDirIsNotAnError(t *testing.T) {
	n, err := sweepOrphanedConfigsIn(filepath.Join(t.TempDir(), "absent"), OrphanedConfigMaxAge, time.Now())
	require.NoError(t, err)
	require.Zero(t, n)
}
