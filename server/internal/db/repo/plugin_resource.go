package repo

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/plugin"
)

// ReconcilePluginResources gives every plugin row a registry identity. A plugin
// primary key is its manifest id — a human-authored value — so the registry row
// records it in origin_ref and the generated UUID becomes the stable identity
// that survives a manifest rename.
//
// Idempotent: a plugin that already carries a resource_id is skipped, so this
// runs on every boot and returns 0 once the tree is settled.
//
// A plugin whose id fails the registry's slug validation (e.g. predates the
// 64-character cap) is logged and skipped rather than aborting the whole
// reconcile — one bad row must not stop every plugin ordered after it from
// ever receiving a registry identity.
func ReconcilePluginResources(ctx context.Context, resources ResourceRepo, client *ent.Client) (int, error) {
	rows, err := client.Plugin.Query().Where(plugin.ResourceIDEQ("")).All(ctx)
	if err != nil {
		return 0, fmt.Errorf("reconcile plugins: query unlinked: %w", err)
	}

	linked, skipped := 0, 0
	for _, p := range rows {
		state := ResourceStateDiscovered
		switch {
		case p.Active:
			state = ResourceStateEnabled
		case p.InstalledAt != nil:
			state = ResourceStateDisabled
		}

		res, err := resources.Upsert(ctx, UpsertResourceInput{
			Kind:      ResourceKindApplication,
			Slug:      p.ID,
			Name:      p.Name,
			Scope:     GlobalScope(),
			State:     state,
			Version:   p.Version,
			Origin:    ResourceOriginLocal,
			OriginRef: p.ID,
		})
		if err != nil {
			skipped++
			slog.Warn("reconcile plugin: skipped", "plugin_id", p.ID, "err", err)
			continue
		}
		if err := client.Plugin.UpdateOneID(p.ID).SetResourceID(res.ID).Exec(ctx); err != nil {
			skipped++
			slog.Warn("reconcile plugin: skipped", "plugin_id", p.ID, "err", err)
			continue
		}
		linked++
	}
	if skipped > 0 {
		slog.Warn("reconcile plugins: some plugins were not linked", "linked", linked, "skipped", skipped)
	}
	return linked, nil
}
