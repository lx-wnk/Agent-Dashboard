package pluginlifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

// DiscoveredPlugin is the upsert payload for one found manifest.
type DiscoveredPlugin struct {
	ID, Name, Version, Path, ManifestHash string
}

// DiscoverRepo upserts a discovered plugin and reports whether its manifest
// changed since the stored row (update-available). It also exposes the stored
// ids and a removal hook so discovery can reconcile rows whose manifest is gone.
type DiscoverRepo interface {
	UpsertDiscovered(ctx context.Context, in DiscoveredPlugin) (updateAvailable bool, err error)
	ExistingIDs(ctx context.Context) ([]string, error)
	// IsInstalled reports whether the plugin has been installed (installed_at != nil).
	// Installed plugins whose manifest vanishes are orphaned, not deleted.
	IsInstalled(ctx context.Context, id string) (bool, error)
	Remove(ctx context.Context, id string) error
}

type Discoverer struct {
	dir  string
	repo DiscoverRepo
}

func NewDiscoverer(dir string, repo DiscoverRepo) *Discoverer { return &Discoverer{dir: dir, repo: repo} }

// Result summarizes a discovery pass.
type Result struct {
	Found            int
	UpdatesAvailable []string
	Removed          []string
}

func (d *Discoverer) Discover(ctx context.Context) (Result, error) {
	var res Result
	if d.dir == "" {
		return res, nil
	}
	found := map[string]bool{}
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return res, fmt.Errorf("discover: read dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(d.dir, e.Name())
		raw, err := os.ReadFile(filepath.Join(path, "plugin.json"))
		if err != nil {
			continue // no manifest — skip (existing behavior)
		}
		var desc plugin.Descriptor
		if err := json.Unmarshal(raw, &desc); err != nil || desc.ID == "" {
			continue
		}
		sum := sha256.Sum256(raw)
		hash := hex.EncodeToString(sum[:])
		upd, err := d.repo.UpsertDiscovered(ctx, DiscoveredPlugin{
			ID: desc.ID, Name: desc.Name, Version: desc.Version, Path: path, ManifestHash: hash,
		})
		if err != nil {
			return res, err
		}
		found[desc.ID] = true
		res.Found++
		if upd {
			res.UpdatesAvailable = append(res.UpdatesAvailable, desc.ID)
		}
	}
	existing, err := d.repo.ExistingIDs(ctx)
	if err != nil {
		return res, fmt.Errorf("discover: existing ids: %w", err)
	}
	// Guard: zero manifests found means the plugin dir is absent or misconfigured,
	// not that all plugins were removed — skip reconciliation to prevent data loss.
	if len(found) == 0 {
		return res, nil
	}
	for _, id := range existing {
		if found[id] {
			continue
		}
		installed, err := d.repo.IsInstalled(ctx, id)
		if err != nil {
			return res, fmt.Errorf("discover: check installed %q: %w", id, err)
		}
		if installed {
			// Retain the row and its settings; the plugin is orphaned until manually uninstalled.
			continue
		}
		if err := d.repo.Remove(ctx, id); err != nil {
			return res, fmt.Errorf("discover: remove %q: %w", id, err)
		}
		res.Removed = append(res.Removed, id)
	}
	return res, nil
}
