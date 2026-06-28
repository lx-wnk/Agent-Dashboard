package pluginlifecycle

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memDiscoverRepo struct {
	rows      map[string]DiscoverRow
	installed map[string]bool // ids with installed_at set
}
type DiscoverRow struct{ Version, ManifestHash string }

func (m *memDiscoverRepo) UpsertDiscovered(_ context.Context, in DiscoveredPlugin) (bool, error) {
	prev, existed := m.rows[in.ID]
	m.rows[in.ID] = DiscoverRow{in.Version, in.ManifestHash}
	updateAvailable := existed && prev.ManifestHash != in.ManifestHash
	return updateAvailable, nil
}

func (m *memDiscoverRepo) ExistingIDs(_ context.Context) ([]string, error) {
	ids := make([]string, 0, len(m.rows))
	for id := range m.rows {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *memDiscoverRepo) IsInstalled(_ context.Context, id string) (bool, error) {
	return m.installed[id], nil
}

func (m *memDiscoverRepo) Remove(_ context.Context, id string) error {
	delete(m.rows, id)
	return nil
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

func TestDiscover_ReconcilesRemovedPlugins(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "p1", "1.0.0")
	writeManifest(t, dir, "p2", "1.0.0")
	repo := &memDiscoverRepo{rows: map[string]DiscoverRow{}}
	d := NewDiscoverer(dir, repo)

	res, err := d.Discover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, res.Found)
	assert.Empty(t, res.Removed)

	// p2's directory disappears → second pass reconciles it away
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "p2")))
	res, err = d.Discover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, res.Found)
	assert.Contains(t, res.Removed, "p2")
	assert.NotContains(t, repo.rows, "p2")
	assert.Contains(t, repo.rows, "p1")
}

func TestDiscover_EmptyDirDoesNotDeleteRows(t *testing.T) {
	emptyDir := t.TempDir() // no manifests inside
	repo := &memDiscoverRepo{rows: map[string]DiscoverRow{
		"p1": {Version: "1.0.0", ManifestHash: "abc"},
	}}
	d := NewDiscoverer(emptyDir, repo)

	res, err := d.Discover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, res.Found)
	assert.Empty(t, res.Removed)
	assert.Contains(t, repo.rows, "p1") // row must survive
}

// TestDiscover_InstalledPluginOrphanedNotDeleted verifies that a plugin whose
// manifest disappears from disk is retained when it has been installed
// (installed_at != nil). A discovered-only (uninstalled) absent plugin is still
// removed. Settings rows are implied by the plugin row surviving.
func TestDiscover_InstalledPluginOrphanedNotDeleted(t *testing.T) {
	dir := t.TempDir()
	// Only p1 has a manifest on disk; p2 (installed) and p3 (not installed) are absent.
	writeManifest(t, dir, "p1", "1.0.0")
	repo := &memDiscoverRepo{
		rows: map[string]DiscoverRow{
			"p1": {Version: "1.0.0", ManifestHash: "old"},
			"p2": {Version: "2.0.0", ManifestHash: "old"}, // installed, manifest gone
			"p3": {Version: "3.0.0", ManifestHash: "old"}, // not installed, manifest gone
		},
		installed: map[string]bool{"p2": true},
	}
	d := NewDiscoverer(dir, repo)

	res, err := d.Discover(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1, res.Found)
	assert.Contains(t, repo.rows, "p1")
	assert.Contains(t, repo.rows, "p2",
		"installed plugin must survive discovery pass even when manifest is absent")
	assert.NotContains(t, repo.rows, "p3",
		"non-installed absent plugin must be removed")
	assert.Contains(t, res.Removed, "p3")
	assert.NotContains(t, res.Removed, "p2")
}
