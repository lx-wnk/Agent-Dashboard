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

func TestOrchestrator_BacklogTransitionsToImplementation(t *testing.T) {
	// This test exercises the backlog stage handler end-to-end through ProgressTask.
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	client := bundle.Client
	defer client.Close() //nolint:errcheck

	ctx := context.Background()
	taskRepo := repo.NewTaskRepo(client)
	srRepo := repo.NewStageRunRepo(client)
	permRepo := repo.NewPermissionRepo(client)
	auditRepo := repo.NewAuditRepo(client)
	cfgRepo := repo.NewPipelineConfigRepo(client)

	orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
		PollInterval:   100 * time.Millisecond,
		TaskRepo:       taskRepo,
		StageRunRepo:   srRepo,
		PermissionRepo: permRepo,
		AuditRepo:      auditRepo,
		ConfigRepo:     cfgRepo,
	})
	require.NoError(t, err)

	task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug:                "backlog-test",
		Title:               "Backlog Test",
		Cwd:                 "/tmp",
		CurrentStage:        "backlog",
		Priority:            "medium",
		MaxIterations:       3,
		StageTimeoutSeconds: 1800,
	})
	require.NoError(t, err)

	sr, err := orch.ProgressTask(ctx, task.ID, nil)
	require.NoError(t, err)
	require.NotNil(t, sr)
	require.Equal(t, "done", sr.Status) // backlog stage_run is done after transitioning

	updated, err := taskRepo.GetByID(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, "implementation", updated.CurrentStage)
}

func TestOrchestrator_AsyncRunningTransition_RecordsPI(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	client := bundle.Client
	defer client.Close() //nolint:errcheck

	ctx := context.Background()
	taskRepo := repo.NewTaskRepo(client)
	srRepo := repo.NewStageRunRepo(client)
	permRepo := repo.NewPermissionRepo(client)
	auditRepo := repo.NewAuditRepo(client)
	cfgRepo := repo.NewPipelineConfigRepo(client)

	// Stub implementation handler: returns async_running with PID 42
	stubHandler := &stubStageHandler{stage: "implementation", transition: pipeline.AsyncRunningTransition{PID: 42}}

	orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
		TaskRepo:       taskRepo,
		StageRunRepo:   srRepo,
		PermissionRepo: permRepo,
		AuditRepo:      auditRepo,
		ConfigRepo:     cfgRepo,
	})
	require.NoError(t, err)
	orch.SetHandlerOverride("implementation", stubHandler)

	task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug:                "impl-test",
		Title:               "Impl Test",
		Cwd:                 "/tmp",
		CurrentStage:        "implementation",
		Priority:            "medium",
		MaxIterations:       3,
		StageTimeoutSeconds: 1800,
	})
	require.NoError(t, err)

	sr, err := orch.ProgressTask(ctx, task.ID, nil)
	require.NoError(t, err)
	require.NotNil(t, sr)
	require.Equal(t, "running", sr.Status)
	require.NotNil(t, sr.Pid)
	require.Equal(t, 42, *sr.Pid)
}

func TestOrchestrator_FailTransition_TaskStageUnchanged(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	client := bundle.Client
	defer client.Close() //nolint:errcheck
	ctx := context.Background()

	taskRepo := repo.NewTaskRepo(client)
	srRepo := repo.NewStageRunRepo(client)
	permRepo := repo.NewPermissionRepo(client)
	auditRepo := repo.NewAuditRepo(client)
	cfgRepo := repo.NewPipelineConfigRepo(client)

	stubHandler := &stubStageHandler{stage: "implementation", transition: pipeline.FailTransition{Reason: "test failure"}}

	orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
		TaskRepo:       taskRepo,
		StageRunRepo:   srRepo,
		PermissionRepo: permRepo,
		AuditRepo:      auditRepo,
		ConfigRepo:     cfgRepo,
	})
	require.NoError(t, err)
	orch.SetHandlerOverride("implementation", stubHandler)

	task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug:                "fail-test",
		Title:               "Fail Test",
		Cwd:                 "/tmp",
		CurrentStage:        "implementation",
		Priority:            "medium",
		MaxIterations:       3,
		StageTimeoutSeconds: 1800,
	})
	require.NoError(t, err)

	sr, err := orch.ProgressTask(ctx, task.ID, nil)
	require.NoError(t, err)
	require.Equal(t, "failed", sr.Status)

	// Task stage must stay at implementation (not advance on failure)
	updated, err := taskRepo.GetByID(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, "implementation", updated.CurrentStage)
}

// stubStageHandler is a test double that returns a predetermined transition.
type stubStageHandler struct {
	stage      string
	transition pipeline.StageTransition
}

func (h *stubStageHandler) Stage() string       { return h.stage }
func (h *stubStageHandler) RequiresAgent() bool { return false }
func (h *stubStageHandler) Execute(_ *pipeline.StageContext) (pipeline.StageTransition, error) {
	return h.transition, nil
}
