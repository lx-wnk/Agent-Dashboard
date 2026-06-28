package pluginlifecycle

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memDiscoverRepo struct{ rows map[string]DiscoverRow }
type DiscoverRow struct{ Version, ManifestHash string }

func (m *memDiscoverRepo) UpsertDiscovered(_ context.Context, in DiscoveredPlugin) (bool, error) {
	prev, existed := m.rows[in.ID]
	m.rows[in.ID] = DiscoverRow{in.Version, in.ManifestHash}
	updateAvailable := existed && prev.ManifestHash != in.ManifestHash
	return updateAvailable, nil
}

func writeManifest(t *testing.T, dir, id, version string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, id), 0o755))
	body := `{"id":"` + id + `","version":"` + version + `","capabilities":["route_extension"],"addr":"127.0.0.1:1"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, id, "plugin.json"), []byte(body), 0o644))
}

func TestDiscover_UpsertsAndDetectsChange(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "p1", "1.0.0")
	repo := &memDiscoverRepo{rows: map[string]DiscoverRow{}}
	d := NewDiscoverer(dir, repo)

	res, err := d.Discover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, res.Found)
	assert.Empty(t, res.UpdatesAvailable)

	// version bump → manifest hash change → update available
	writeManifest(t, dir, "p1", "2.0.0")
	res, err = d.Discover(context.Background())
	require.NoError(t, err)
	assert.Contains(t, res.UpdatesAvailable, "p1")
}
