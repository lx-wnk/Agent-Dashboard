package pipeline_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

// makeOrchestratorFull returns an orchestrator plus all repos for sweep tests.
func makeOrchestratorFull(t *testing.T) (*pipeline.PipelineOrchestrator, repo.TaskRepo, repo.StageRunRepo) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	taskRepo := repo.NewTaskRepo(bundle.Client)
	srRepo := repo.NewStageRunRepo(bundle.Client)
	permRepo := repo.NewPermissionRepo(bundle.Client)
	auditRepo := repo.NewAuditEventRepo(bundle.Client)
	cfgRepo := repo.NewPipelineConfigRepo(bundle.Client)

	orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
		TaskRepo:       taskRepo,
		StageRunRepo:   srRepo,
		PermissionRepo: permRepo,
		AuditRepo:      auditRepo,
		ConfigRepo:     cfgRepo,
	})
	require.NoError(t, err)
	return orch, taskRepo, srRepo
}

// makeRequeuedRun creates a task + stage_run in "requeued" status with the given nextRetryAt.
func makeRequeuedRun(t *testing.T, ctx context.Context, taskRepo repo.TaskRepo, srRepo repo.StageRunRepo, nextRetryAt time.Time) (*repo.CreateTaskInput, string) {
	t.Helper()
	task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug:                "requeue-sweep-test",
		Title:               "Requeue Sweep Test",
		Cwd:                 "/tmp",
		CurrentStage:        "implementation",
		Priority:            "medium",
		MaxIterations:       3,
		StageTimeoutSeconds: 1800,
	})
	require.NoError(t, err)

	sr, err := srRepo.Create(ctx, repo.CreateStageRunInput{
		TaskID:      task.ID,
		Stage:       "implementation",
		Iteration:   0,
		SessionName: "requeue-sweep-impl-0",
	})
	require.NoError(t, err)

	retryCount := 1
	startedAt := time.Now().Add(-5 * time.Minute)
	deadPID := 99999
	sr, err = srRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
		Status:      strPtr("requeued"),
		PID:         &deadPID,
		StartedAt:   &startedAt,
		RetryCount:  &retryCount,
		NextRetryAt: &nextRetryAt,
	})
	require.NoError(t, err)

	_ = sr
	return nil, sr.ID
}

func TestSweepRequeueableRuns_PastCooldown_PromotesToPending(t *testing.T) {
	ctx := context.Background()
	orch, taskRepo, srRepo := makeOrchestratorFull(t)

	past := time.Now().Add(-1 * time.Minute)
	_, runID := makeRequeuedRun(t, ctx, taskRepo, srRepo, past)

	before, err := srRepo.GetByID(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, 1, before.RetryCount)

	err = orch.SweepRequeueableRunsForTest(ctx)
	require.NoError(t, err)

	updated, err := srRepo.GetByID(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, "pending", updated.Status)
	require.Nil(t, updated.Pid, "PID must be cleared")
	require.Nil(t, updated.StartedAt, "started_at must be cleared")
	require.Nil(t, updated.NextRetryAt, "next_retry_at must be cleared")
	require.Equal(t, 1, updated.RetryCount, "retry_count must be preserved")
}

func TestSweepRequeueableRuns_FutureCooldown_Unchanged(t *testing.T) {
	ctx := context.Background()
	orch, taskRepo, srRepo := makeOrchestratorFull(t)

	future := time.Now().Add(10 * time.Minute)
	_, runID := makeRequeuedRun(t, ctx, taskRepo, srRepo, future)

	err := orch.SweepRequeueableRunsForTest(ctx)
	require.NoError(t, err)

	updated, err := srRepo.GetByID(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, "requeued", updated.Status, "run with future cooldown must not be promoted")
}

func TestSweepOrphanRuns_RequeuedRunOnParkedTask_Reaped(t *testing.T) {
	ctx := context.Background()
	orch, taskRepo, srRepo := makeOrchestratorFull(t)

	// Create a task in "cancelled" state (parked).
	task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug:                "orphan-requeued-test",
		Title:               "Orphan Requeued Test",
		Cwd:                 "/tmp",
		CurrentStage:        "cancelled",
		Priority:            "medium",
		MaxIterations:       3,
		StageTimeoutSeconds: 1800,
	})
	require.NoError(t, err)

	sr, err := srRepo.Create(ctx, repo.CreateStageRunInput{
		TaskID:      task.ID,
		Stage:       "implementation",
		Iteration:   0,
		SessionName: "orphan-requeued-impl-0",
	})
	require.NoError(t, err)

	retryCount := 1
	future := time.Now().Add(5 * time.Minute)
	_, err = srRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
		Status:      strPtr("requeued"),
		RetryCount:  &retryCount,
		NextRetryAt: &future,
	})
	require.NoError(t, err)

	err = orch.SweepOrphanRunsForTest(ctx, nil)
	require.NoError(t, err)

	updated, err := srRepo.GetByID(ctx, sr.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", updated.Status, "requeued run on parked task must be reaped to failed")
}
