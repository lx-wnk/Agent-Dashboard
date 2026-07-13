package pipeline

import (
	"context"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

const (
	// Per-stage model config key prefix (e.g. stageModelKeyPrefix+"implementation").
	stageModelKeyPrefix = "stageModel."

	// Balanced defaults: implementation gets the most capable model, finalization
	// the fastest. An explicit DB row or task/spawner override takes precedence.
	defaultModelImplementation = "claude-opus-4-6"
	defaultModelSelfReview     = "claude-sonnet-4-6"
	defaultModelPlanReview     = "claude-sonnet-4-6"
	defaultModelFinalization   = "claude-haiku-4-5"
)

// modelResolver resolves the effective per-stage model, applying coded default
// → global config row → project config row precedence.
type modelResolver struct {
	cache *configCache
	repo  repo.PipelineConfigRepo
}

// newModelResolver constructs a modelResolver. cache must be the same
// *configCache instance the orchestrator uses, so cache invalidation stays coherent.
func newModelResolver(cache *configCache, cfgRepo repo.PipelineConfigRepo) *modelResolver {
	return &modelResolver{cache: cache, repo: cfgRepo}
}

// StageDefault returns the effective per-stage model string for the given
// project scope. Precedence: coded default → global config row → project config row
// (project→global→coded via GetStringScoped). Caller applies task/spawner override on top.
func (r *modelResolver) StageDefault(ctx context.Context, stage string, projectID *string) string {
	var coded string
	switch stage {
	case "implementation":
		coded = defaultModelImplementation
	case "self_review":
		coded = defaultModelSelfReview
	case "plan_review":
		coded = defaultModelPlanReview
	case "finalization":
		coded = defaultModelFinalization
	}
	if projectID == nil {
		// Global-only path: use the cached global lookup.
		return r.cache.String(ctx, stageModelKeyPrefix+stage, coded)
	}
	// Project-scoped path: project row → global row → coded default (no cache bypass needed).
	return r.repo.GetStringScoped(ctx, projectID, stageModelKeyPrefix+stage, coded)
}
