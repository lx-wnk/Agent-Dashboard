package pipeline_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

// modelCaptureSpawnFn returns a spawnFn that captures the opts it was called with.
func modelCaptureSpawnFn(captured *pipeline.SpawnAgentOptions) func(pipeline.SpawnAgentOptions) (pipeline.SpawnResult, error) {
	return func(opts pipeline.SpawnAgentOptions) (pipeline.SpawnResult, error) {
		*captured = opts
		return pipeline.SpawnResult{PID: 1}, nil
	}
}

// openBundle creates an in-memory SQLite DB for a single test.
func openBundle(t *testing.T) *db.DBBundle {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	return bundle
}

// makeOrchWithConfig builds an orchestrator backed by the given bundle,
// wiring all required repos.
func makeOrchWithConfig(t *testing.T, bundle *db.DBBundle) (*pipeline.PipelineOrchestrator, repo.TaskRepo, repo.PipelineConfigRepo) {
	t.Helper()
	c := bundle.Client
	cfgRepo := repo.NewPipelineConfigRepo(c)
	orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
		TaskRepo:       repo.NewTaskRepo(c),
		StageRunRepo:   repo.NewStageRunRepo(c),
		PermissionRepo: repo.NewPermissionRepo(c),
		AuditRepo:      repo.NewAuditEventRepo(c),
		ConfigRepo:     cfgRepo,
	})
	require.NoError(t, err)
	return orch, repo.NewTaskRepo(c), cfgRepo
}

// TestStageModelDefault_BalancedDefaults verifies that the coded Balanced defaults are applied
// for each agent-driven stage when no task/spawner model override is present.
func TestStageModelDefault_BalancedDefaults(t *testing.T) {
	cases := []struct {
		stage     string
		wantModel string
	}{
		{"implementation", "claude-opus-4-6"},
		{"self_review", "claude-sonnet-4-6"},
		{"finalization", "claude-haiku-4-5"},
	}

	for _, tc := range cases {
		t.Run(tc.stage, func(t *testing.T) {
			bundle := openBundle(t)
			_, _, _ = makeOrchWithConfig(t, bundle)

			// Drive through Execute directly to capture opts.Model.
			// Build a minimal StageContext that wires StageModelFn to the
			// real orchestrator's method via a fresh orchestrator.
			orch, taskRepo, _ := makeOrchWithConfig(t, bundle)

			ctx := context.Background()
			task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
				Slug:                "model-default-" + tc.stage,
				Title:               "Model default test",
				Cwd:                 "/tmp",
				CurrentStage:        tc.stage,
				Priority:            "medium",
				MaxIterations:       3,
				StageTimeoutSeconds: 1800,
			})
			require.NoError(t, err)

			var capturedOpts pipeline.SpawnAgentOptions
			spawnFn := modelCaptureSpawnFn(&capturedOpts)

			handler := pipeline.NewAgentStageHandlerForTest(tc.stage, spawnFn)
			orch.SetHandlerOverride(tc.stage, handler)

			_, _ = orch.ProgressTask(ctx, task.ID, nil)

			assert.Equal(t, tc.wantModel, capturedOpts.Model,
				"stage %s: expected Balanced default model", tc.stage)
		})
	}
}

// TestStageModelDefault_SpawnerModelOverrideWins verifies that Spawner.ModelOverride
// takes precedence over the per-stage Balanced default.
func TestStageModelDefault_SpawnerModelOverrideWins(t *testing.T) {
	bundle := openBundle(t)
	orch, taskRepo, _ := makeOrchWithConfig(t, bundle)
	ctx := context.Background()

	spawnerModel := "claude-sonnet-4-6"
	task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug:                "spawner-override",
		Title:               "Spawner override test",
		Cwd:                 "/tmp",
		CurrentStage:        "implementation",
		Priority:            "medium",
		MaxIterations:       3,
		StageTimeoutSeconds: 1800,
	})
	require.NoError(t, err)

	var capturedOpts pipeline.SpawnAgentOptions
	spawnFn := modelCaptureSpawnFn(&capturedOpts)

	// Spawner with ModelOverride set — this should beat the Balanced default (Opus).
	orch.SetResolveSpawner(func(_ context.Context, _ string) (*ent.Spawner, error) {
		return &ent.Spawner{
			Slug:          "test-spawner",
			AdapterType:   "claude",
			ModelOverride: &spawnerModel,
		}, nil
	})

	handler := pipeline.NewAgentStageHandlerForTest("implementation", spawnFn)
	orch.SetHandlerOverride("implementation", handler)

	_, _ = orch.ProgressTask(ctx, task.ID, nil)

	// spawnerHasOverride=true → nativeModel stays "" → BuildSpawnArgs uses ModelOverride.
	assert.Empty(t, capturedOpts.Model,
		"opts.Model must be empty when spawner declares override so BuildSpawnArgs applies it")
}

// TestStageModelDefault_DBConfigRowOverridesCoded verifies that a pipeline_config row
// stageModel.<stage> overrides the coded Balanced default for that stage.
func TestStageModelDefault_DBConfigRowOverridesCoded(t *testing.T) {
	bundle := openBundle(t)
	orch, taskRepo, cfgRepo := makeOrchWithConfig(t, bundle)
	ctx := context.Background()

	customModel := "claude-sonnet-4-6"
	err := cfgRepo.Set(ctx, "stageModel.implementation", customModel)
	require.NoError(t, err)

	// Invalidate cache so the freshly written row is visible.
	orch.InvalidateConfigCache()

	task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug:                "config-override",
		Title:               "Config override test",
		Cwd:                 "/tmp",
		CurrentStage:        "implementation",
		Priority:            "medium",
		MaxIterations:       3,
		StageTimeoutSeconds: 1800,
	})
	require.NoError(t, err)

	var capturedOpts pipeline.SpawnAgentOptions
	spawnFn := modelCaptureSpawnFn(&capturedOpts)
	handler := pipeline.NewAgentStageHandlerForTest("implementation", spawnFn)
	orch.SetHandlerOverride("implementation", handler)

	_, _ = orch.ProgressTask(ctx, task.ID, nil)

	assert.Equal(t, customModel, capturedOpts.Model,
		"DB config row stageModel.implementation should override the Opus default")
}
