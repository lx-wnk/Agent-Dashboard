package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// claudeDefaultSpawnerSlug is the slug of the built-in spawner that exists in
// every dashboard installation. It is the final fallback in the spawner
// resolution chain (task.spawnerId ?? project.defaultSpawnerId ?? claude-default).
const claudeDefaultSpawnerSlug = "claude-default"

// seedSpawners inserts the built-in claude-default spawner if it is not already
// present. Idempotent: a no-op once the row exists.
func seedSpawners(ctx context.Context, spawnerRepo repo.SpawnerRepo) error {
	if spawnerRepo == nil {
		return nil
	}
	_, err := spawnerRepo.GetBySlug(ctx, claudeDefaultSpawnerSlug)
	if err == nil {
		return nil
	}
	if !ent.IsNotFound(err) {
		return fmt.Errorf("seedSpawners: lookup claude-default: %w", err)
	}

	description := "Built-in Claude CLI spawner — cannot be deleted."
	if _, err := spawnerRepo.Create(
		ctx,
		"Claude (default)",
		claudeDefaultSpawnerSlug,
		"claude",
		[]string{},
		map[string]string{},
		nil,
		&description,
		true,
	); err != nil {
		return fmt.Errorf("seedSpawners: create claude-default: %w", err)
	}
	slog.Info("seeded built-in spawner", "slug", claudeDefaultSpawnerSlug)
	return nil
}
