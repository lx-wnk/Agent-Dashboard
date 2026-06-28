package main

import (
	"context"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pluginlifecycle"
	"github.com/lx-wnk/agent-dashboard/server/internal/pluginsettings"
)

// pluginSettingRepoAdapter maps the ent-backed PluginSettingRepo onto the
// pluginsettings.Repo interface (Stored ↔ PluginSettingInput/ent.PluginSetting).
type pluginSettingRepoAdapter struct{ inner repo.PluginSettingRepo }

func (a pluginSettingRepoAdapter) ListByPlugin(ctx context.Context, pluginID string) ([]pluginsettings.Stored, error) {
	rows, err := a.inner.ListByPlugin(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	out := make([]pluginsettings.Stored, len(rows))
	for i, r := range rows {
		out[i] = pluginsettings.Stored{Key: r.Key, Value: r.Value, Nonce: r.Nonce, Secret: r.Secret}
	}
	return out, nil
}

func (a pluginSettingRepoAdapter) Upsert(ctx context.Context, pluginID string, s pluginsettings.Stored) error {
	_, err := a.inner.Upsert(ctx, repo.PluginSettingInput{
		PluginID: pluginID, Key: s.Key, Value: s.Value, Nonce: s.Nonce, Secret: s.Secret,
	})
	return err
}

func (a pluginSettingRepoAdapter) DeleteByPlugin(ctx context.Context, pluginID string) error {
	return a.inner.DeleteByPlugin(ctx, pluginID)
}

// pluginStateRepoAdapter maps the ent-backed PluginRepo onto the
// pluginlifecycle.StateRepo interface (ent.Plugin → pluginlifecycle.State).
type pluginStateRepoAdapter struct{ inner repo.PluginRepo }

func (a pluginStateRepoAdapter) GetState(ctx context.Context, id string) (pluginlifecycle.State, error) {
	p, err := a.inner.Get(ctx, id)
	if err != nil {
		return pluginlifecycle.State{}, err
	}
	return pluginlifecycle.State{InstalledAt: p.InstalledAt, Active: p.Active, Version: p.Version}, nil
}

func (a pluginStateRepoAdapter) SetInstalledAt(ctx context.Context, id string, at *time.Time) error {
	return a.inner.SetInstalledAt(ctx, id, at)
}

func (a pluginStateRepoAdapter) SetActive(ctx context.Context, id string, active bool) error {
	return a.inner.SetActive(ctx, id, active)
}

func (a pluginStateRepoAdapter) SetVersion(ctx context.Context, id, version string) error {
	return a.inner.SetVersion(ctx, id, version)
}

// pluginDiscoverRepoAdapter maps the ent-backed PluginRepo onto the
// pluginlifecycle.DiscoverRepo interface. It reads the prior manifest hash to
// report update-available, then upserts (the upsert preserves installed_at/active).
type pluginDiscoverRepoAdapter struct{ inner repo.PluginRepo }

func (a pluginDiscoverRepoAdapter) UpsertDiscovered(ctx context.Context, in pluginlifecycle.DiscoveredPlugin) (bool, error) {
	var oldHash string
	if existing, err := a.inner.Get(ctx, in.ID); err == nil {
		oldHash = existing.ManifestHash
	} else if !repo.IsNotFound(err) {
		return false, err
	}
	if _, err := a.inner.Upsert(ctx, repo.UpsertPluginInput{
		ID: in.ID, Name: in.Name, Version: in.Version, Path: in.Path, ManifestHash: in.ManifestHash,
	}); err != nil {
		return false, err
	}
	return oldHash != in.ManifestHash, nil
}
