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
// changed since the stored row (update-available).
type DiscoverRepo interface {
	UpsertDiscovered(ctx context.Context, in DiscoveredPlugin) (updateAvailable bool, err error)
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
}

func (d *Discoverer) Discover(ctx context.Context) (Result, error) {
	var res Result
	if d.dir == "" {
		return res, nil
	}
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
		res.Found++
		if upd {
			res.UpdatesAvailable = append(res.UpdatesAvailable, desc.ID)
		}
	}
	return res, nil
}
