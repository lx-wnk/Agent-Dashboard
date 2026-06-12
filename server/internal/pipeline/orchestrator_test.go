package pipeline_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

func TestOrchestrator_BacklogTransitionsToImplementation(t *testing.T) {
	orch, taskRepo := makeTestOrchestratorWithRepos(t)
	ctx := context.Background()

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
	require.Equal(t, "done", sr.Status)

	updated, err := taskRepo.GetByID(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, "implementation", updated.CurrentStage)
}

func TestOrchestrator_AsyncRunningTransition_RecordsPI(t *testing.T) {
	orch, taskRepo := makeTestOrchestratorWithRepos(t)
	orch.SetHandlerOverride("implementation", &stubStageHandler{
		stage:      "implementation",
		transition: pipeline.AsyncRunningTransition{PID: 42},
	})

	task, err := taskRepo.Create(context.Background(), repo.CreateTaskInput{
		Slug:                "impl-test",
		Title:               "Impl Test",
		Cwd:                 "/tmp",
		CurrentStage:        "implementation",
		Priority:            "medium",
		MaxIterations:       3,
		StageTimeoutSeconds: 1800,
	})
	require.NoError(t, err)

	sr, err := orch.ProgressTask(context.Background(), task.ID, nil)
	require.NoError(t, err)
	require.NotNil(t, sr)
	require.Equal(t, "running", sr.Status)
	require.NotNil(t, sr.Pid)
	require.Equal(t, 42, *sr.Pid)
}

func TestOrchestrator_FailTransition_TaskStageUnchanged(t *testing.T) {
	orch, taskRepo := makeTestOrchestratorWithRepos(t)
	orch.SetHandlerOverride("implementation", &stubStageHandler{
		stage:      "implementation",
		transition: pipeline.FailTransition{Reason: "test failure"},
	})

	task, err := taskRepo.Create(context.Background(), repo.CreateTaskInput{
		Slug:                "fail-test",
		Title:               "Fail Test",
		Cwd:                 "/tmp",
		CurrentStage:        "implementation",
		Priority:            "medium",
		MaxIterations:       3,
		StageTimeoutSeconds: 1800,
	})
	require.NoError(t, err)

	sr, err := orch.ProgressTask(context.Background(), task.ID, nil)
	require.NoError(t, err)
	require.Equal(t, "failed", sr.Status)

	updated, err := taskRepo.GetByID(context.Background(), task.ID)
	require.NoError(t, err)
	require.Equal(t, "implementation", updated.CurrentStage)
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

func TestSortPickCandidates_LowerRankFirst(t *testing.T) {
	// Give the lower-rank task a NEWER created_at to prove rank beats created_at.
	lowRank := ptr(100.0)
	highRank := ptr(900.0)
	newerTime := time.Now()
	olderTime := newerTime.Add(-10 * time.Minute)

	lowRankTask := &ent.Task{ID: "low-rank", CurrentStage: "implementation", Priority: "medium", SilverBullet: false, Rank: lowRank, CreatedAt: newerTime}
	highRankTask := &ent.Task{ID: "high-rank", CurrentStage: "implementation", Priority: "medium", SilverBullet: false, Rank: highRank, CreatedAt: olderTime}

	// Place high-rank task first to confirm sort moves low-rank to front.
	tasks := []*ent.Task{highRankTask, lowRankTask}
	pipeline.SortPickCandidatesForTest(tasks)

	require.Equal(t, "low-rank", tasks[0].ID, "lower rank must sort first regardless of created_at")
	require.Equal(t, "high-rank", tasks[1].ID)
}

func TestSortPickCandidates_PriorityBeatsRank(t *testing.T) {
	// High priority + high rank must still sort before low priority + low rank.
	highRank := ptr(999999.0)
	lowRank := ptr(1.0)
	now := time.Now()

	highPriority := &ent.Task{ID: "high-prio", CurrentStage: "implementation", Priority: "high", SilverBullet: false, Rank: highRank, CreatedAt: now}
	lowPriority := &ent.Task{ID: "low-prio", CurrentStage: "implementation", Priority: "low", SilverBullet: false, Rank: lowRank, CreatedAt: now}

	tasks := []*ent.Task{lowPriority, highPriority}
	pipeline.SortPickCandidatesForTest(tasks)

	require.Equal(t, "high-prio", tasks[0].ID, "high priority must sort first even with a worse (higher) rank")
	require.Equal(t, "low-prio", tasks[1].ID)
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
