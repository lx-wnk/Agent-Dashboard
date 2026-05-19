package pipeline_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
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

// makeTestOrchestrator opens an in-memory SQLite DB and returns a configured orchestrator.
func makeTestOrchestrator(t *testing.T) *pipeline.PipelineOrchestrator {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	client := bundle.Client
	t.Cleanup(func() { _ = client.Close() })

	taskRepo := repo.NewTaskRepo(client)
	srRepo := repo.NewStageRunRepo(client)
	permRepo := repo.NewPermissionRepo(client)
	auditRepo := repo.NewAuditRepo(client)
	cfgRepo := repo.NewPipelineConfigRepo(client)

	orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
		TaskRepo:       taskRepo,
		StageRunRepo:   srRepo,
		PermissionRepo: permRepo,
		AuditRepo:      auditRepo,
		ConfigRepo:     cfgRepo,
	})
	require.NoError(t, err)
	return orch
}

// --- sortPickCandidates tests ---

func TestSortPickCandidates_SilverBulletFirst(t *testing.T) {
	now := time.Now()
	normal := &ent.Task{ID: "normal", CurrentStage: "implementation", Priority: "high", SilverBullet: false, CreatedAt: now}
	bullet := &ent.Task{ID: "bullet", CurrentStage: "implementation", Priority: "high", SilverBullet: true, CreatedAt: now}

	// Place normal first to confirm sort moves bullet to front.
	tasks := []*ent.Task{normal, bullet}
	pipeline.SortPickCandidatesForTest(tasks)

	require.Equal(t, "bullet", tasks[0].ID, "silver-bullet task must be first")
	require.Equal(t, "normal", tasks[1].ID)
}

func TestSortPickCandidates_FurtherStageFirst(t *testing.T) {
	now := time.Now()
	early := &ent.Task{ID: "early", CurrentStage: "implementation", Priority: "medium", SilverBullet: false, CreatedAt: now}
	later := &ent.Task{ID: "later", CurrentStage: "self_review", Priority: "medium", SilverBullet: false, CreatedAt: now}

	tasks := []*ent.Task{early, later}
	pipeline.SortPickCandidatesForTest(tasks)

	require.Equal(t, "later", tasks[0].ID, "task at further stage must be first")
	require.Equal(t, "early", tasks[1].ID)
}

func TestSortPickCandidates_HigherPriorityFirst(t *testing.T) {
	now := time.Now()
	low := &ent.Task{ID: "low", CurrentStage: "implementation", Priority: "low", SilverBullet: false, CreatedAt: now}
	med := &ent.Task{ID: "med", CurrentStage: "implementation", Priority: "medium", SilverBullet: false, CreatedAt: now}
	high := &ent.Task{ID: "high", CurrentStage: "implementation", Priority: "high", SilverBullet: false, CreatedAt: now}

	tasks := []*ent.Task{low, med, high}
	pipeline.SortPickCandidatesForTest(tasks)

	require.Equal(t, "high", tasks[0].ID, "high priority must be first")
	require.Equal(t, "med", tasks[1].ID, "medium priority must be second")
	require.Equal(t, "low", tasks[2].ID, "low priority must be last")
}

func TestSortPickCandidates_OlderFirst(t *testing.T) {
	older := &ent.Task{ID: "older", CurrentStage: "implementation", Priority: "medium", SilverBullet: false, CreatedAt: time.Now().Add(-10 * time.Minute)}
	newer := &ent.Task{ID: "newer", CurrentStage: "implementation", Priority: "medium", SilverBullet: false, CreatedAt: time.Now()}

	tasks := []*ent.Task{newer, older}
	pipeline.SortPickCandidatesForTest(tasks)

	require.Equal(t, "older", tasks[0].ID, "older task must be first")
	require.Equal(t, "newer", tasks[1].ID)
}

// --- decideCompletedTransition tests ---

func TestDecideCompletedTransition_Finalization(t *testing.T) {
	ctx := context.Background()
	orch := makeTestOrchestrator(t)

	task := &ent.Task{ID: "t1", CurrentStage: "finalization"}
	run := &ent.StageRun{ID: "r1", Stage: "finalization"}

	transition := orch.DecideCompletedTransitionForTest(ctx, task, run, map[string]any{})

	_, ok := transition.(pipeline.DoneTransition)
	require.True(t, ok, "finalization stage must produce DoneTransition, got %T", transition)
}

func TestDecideCompletedTransition_SelfReview_Passed(t *testing.T) {
	ctx := context.Background()
	orch := makeTestOrchestrator(t)

	task := &ent.Task{ID: "t2", CurrentStage: "self_review"}
	run := &ent.StageRun{ID: "r2", Stage: "self_review"}
	output := map[string]any{"passed": true}

	transition := orch.DecideCompletedTransitionForTest(ctx, task, run, output)

	next, ok := transition.(pipeline.NextTransition)
	require.True(t, ok, "passed self_review must produce NextTransition, got %T", transition)
	require.Equal(t, "finalization", next.Stage)
}

func TestDecideCompletedTransition_SelfReview_Passed_ClearsFeedback(t *testing.T) {
	ctx := context.Background()
	orch := makeTestOrchestrator(t)

	task := &ent.Task{
		ID:           "t3",
		CurrentStage: "self_review",
		Metadata:     map[string]any{"review_feedback": "old feedback", "review_cycles": float64(1)},
	}
	run := &ent.StageRun{ID: "r3", Stage: "self_review"}
	output := map[string]any{"passed": true}

	transition := orch.DecideCompletedTransitionForTest(ctx, task, run, output)

	next, ok := transition.(pipeline.NextTransition)
	require.True(t, ok, "passed self_review must produce NextTransition, got %T", transition)
	require.Equal(t, "finalization", next.Stage)

	// review_feedback must not appear in the metadata patch
	if next.MetadataPatch != nil {
		_, hasFeedback := next.MetadataPatch["review_feedback"]
		require.False(t, hasFeedback, "review_feedback must be cleared after passing review")
	}
	// MetaClear is an acceptable alternative when all metadata was feedback-only
	// (the orchestrator sets MetaClear=true when rest is empty)
}

func TestDecideCompletedTransition_SelfReview_Failed_FirstCycle(t *testing.T) {
	ctx := context.Background()
	orch := makeTestOrchestrator(t)

	task := &ent.Task{ID: "t4", CurrentStage: "self_review", Metadata: nil}
	run := &ent.StageRun{ID: "r4", Stage: "self_review"}
	// Provide structured findings so SummarizeReviewFindings returns a non-empty string.
	output := map[string]any{
		"passed":  false,
		"summary": "missing tests and error handling",
		"findings": []any{
			map[string]any{
				"severity":    "ERROR",
				"description": "no unit tests for service layer",
				"file":        "service/foo.go",
			},
		},
	}

	transition := orch.DecideCompletedTransitionForTest(ctx, task, run, output)

	next, ok := transition.(pipeline.NextTransition)
	require.True(t, ok, "failed self_review (first cycle) must produce NextTransition, got %T", transition)
	require.Equal(t, "implementation", next.Stage)
	require.NotEmpty(t, next.MetadataPatch["review_feedback"], "review_feedback must be set in patch")
	require.Equal(t, 1, next.MetadataPatch["review_cycles"], "review_cycles must be 1 after first failure")
}

func TestDecideCompletedTransition_SelfReview_MaxCyclesReached(t *testing.T) {
	ctx := context.Background()
	orch := makeTestOrchestrator(t)

	// defaultMaxReviewCycles is 3; cycles = prevCycles + 1 = 2 + 1 = 3 >= 3 → WaitUser
	task := &ent.Task{
		ID:           "t5",
		CurrentStage: "self_review",
		Metadata:     map[string]any{"review_cycles": float64(2)},
	}
	run := &ent.StageRun{ID: "r5", Stage: "self_review"}
	output := map[string]any{"passed": false}

	transition := orch.DecideCompletedTransitionForTest(ctx, task, run, output)

	_, ok := transition.(pipeline.WaitUserTransition)
	require.True(t, ok, "self_review at max cycles must produce WaitUserTransition, got %T", transition)
}
