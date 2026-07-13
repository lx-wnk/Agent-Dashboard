package pipeline_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

// errInjectedCommit is returned by commitFailTx.Commit to simulate a real
// tx.Commit() failure (e.g. disk full, SQLITE_BUSY on the final fsync).
var errInjectedCommit = errors.New("injected commit failure")

// commitFailTx wraps a real dialect.Tx and forces Commit to fail. Rollback is
// invoked for real so the underlying connection is released — required since
// the in-memory test DB is pinned to a single connection.
type commitFailTx struct {
	dialect.Tx
}

func (t commitFailTx) Commit() error {
	_ = t.Tx.Rollback()
	return errInjectedCommit
}

// commitFailDriver wraps a real dialect.Driver and makes every transaction it
// begins fail on Commit while still executing all statements for real inside
// a genuine SQLite transaction (only the commit itself is faked).
type commitFailDriver struct {
	dialect.Driver
}

func (d commitFailDriver) Tx(ctx context.Context) (dialect.Tx, error) {
	tx, err := d.Driver.Tx(ctx)
	if err != nil {
		return nil, err
	}
	return commitFailTx{Tx: tx}, nil
}

// newCommitFailClient returns an *ent.Client backed by the same underlying
// *sql.DB as the caller's bundle, but whose transactions always fail on Commit.
func newCommitFailClient(sqlDB *sql.DB) *ent.Client {
	drv := entsql.OpenDB(dialect.SQLite, sqlDB)
	return ent.NewClient(ent.Driver(commitFailDriver{Driver: drv}))
}

// TestApplyTransition_CommitFails_PostCommitEffectsDoNotFire is the
// load-bearing CQ-03 regression test. It fails on pre-fix code (postCommit
// closures ran before tx.Commit()) and passes after D1/D2/D3.
func TestApplyTransition_CommitFails_PostCommitEffectsDoNotFire(t *testing.T) {
	ctx := context.Background()
	bundle := openSharedBundle(t)
	c := bundle.Client

	taskRepo := repo.NewTaskRepo(c)
	srRepo := repo.NewStageRunRepo(c)
	events := &capturedEvents{}

	faultyClient := newCommitFailClient(bundle.DB)

	orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
		TaskRepo:       taskRepo,
		StageRunRepo:   srRepo,
		PermissionRepo: repo.NewPermissionRepo(c),
		AuditRepo:      repo.NewAuditEventRepo(c),
		ConfigRepo:     repo.NewPipelineConfigRepo(c),
		Client:         faultyClient,
		OnTaskChanged:  events.record,
	})
	require.NoError(t, err)

	task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug:                "commit-fail",
		Title:               "Commit Fail",
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
		SessionName: "commit-fail-session",
	})
	require.NoError(t, err)

	orch.SeedTaskLockForTest(task.ID)

	result, applyErr := orch.ApplyTransitionForTest(ctx, task, sr, pipeline.DoneTransition{Output: map[string]any{"ok": true}})

	require.Error(t, applyErr, "a failed commit must surface as an error")
	require.ErrorIs(t, applyErr, errInjectedCommit)
	require.Nil(t, result)

	require.Empty(t, events.all(),
		"OnTaskChanged must not fire for a rolled-back transition (neither the dependent-cascade check nor the final broadcast)")
	require.True(t, orch.TaskLockHeldForTest(task.ID),
		"taskLocks.Delete must not run when the commit rolled back")

	reloadedTask, err := taskRepo.GetByID(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, "implementation", reloadedTask.CurrentStage,
		"the transaction rolled back — the task must not have durably reached 'done'")

	reloadedRun, err := srRepo.GetByID(ctx, sr.ID)
	require.NoError(t, err)
	require.NotEqual(t, "done", reloadedRun.Status,
		"the transaction rolled back — the stage run must not have durably reached 'done'")
}

// TestApplyTransition_CommitSucceeds_ClosuresRunAfterCommitBeforeBroadcast verifies
// D3: on a successful commit, postCommit closures (dependent-task cascade) still
// run, and they run before the transition's own OnTaskChanged broadcast.
func TestApplyTransition_CommitSucceeds_ClosuresRunAfterCommitBeforeBroadcast(t *testing.T) {
	ctx := context.Background()
	bundle := openSharedBundle(t)
	c := bundle.Client

	taskRepo := repo.NewTaskRepo(c)
	srRepo := repo.NewStageRunRepo(c)
	depRepo := repo.NewDependencyRepo(c)
	events := &capturedEvents{}

	orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
		TaskRepo:       taskRepo,
		StageRunRepo:   srRepo,
		PermissionRepo: repo.NewPermissionRepo(c),
		AuditRepo:      repo.NewAuditEventRepo(c),
		ConfigRepo:     repo.NewPipelineConfigRepo(c),
		DepRepo:        depRepo,
		Client:         c,
		OnTaskChanged:  events.record,
	})
	require.NoError(t, err)

	upstreamID := makeCascadeTask(t, taskRepo, "tx-order-up")
	downstreamID := makeCascadeTask(t, taskRepo, "tx-order-down")
	_, err = depRepo.Add(ctx, downstreamID, upstreamID, "done", "on_hold")
	require.NoError(t, err)

	upTask, err := taskRepo.GetByID(ctx, upstreamID)
	require.NoError(t, err)

	sr, err := srRepo.Create(ctx, repo.CreateStageRunInput{
		TaskID:      upstreamID,
		Stage:       "finalization",
		Iteration:   0,
		SessionName: "tx-order-session",
	})
	require.NoError(t, err)

	result, applyErr := orch.ApplyTransitionForTest(ctx, upTask, sr, pipeline.DoneTransition{})
	require.NoError(t, applyErr)
	require.NotNil(t, result)

	evs := events.all()
	require.NotEmpty(t, evs)

	last := evs[len(evs)-1]
	require.Equal(t, upstreamID, last.taskID, "the transition's own broadcast must be the last event")
	require.Equal(t, "done", last.reason)

	foundCascade := false
	for _, e := range evs[:len(evs)-1] {
		if e.reason == "dependent_check" {
			foundCascade = true
		}
	}
	require.True(t, foundCascade, "the dependent-cascade check must fire before the final broadcast")
}

// TestApplyTransition_NoTxBranch_ClosuresRunImmediately guards the no-tx test
// path (D2): with Client == nil there is no commit to gate on, so closures
// must still run once, synchronously.
func TestApplyTransition_NoTxBranch_ClosuresRunImmediately(t *testing.T) {
	ctx := context.Background()
	bundle := openSharedBundle(t)
	c := bundle.Client

	taskRepo := repo.NewTaskRepo(c)
	srRepo := repo.NewStageRunRepo(c)
	events := &capturedEvents{}

	orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
		TaskRepo:       taskRepo,
		StageRunRepo:   srRepo,
		PermissionRepo: repo.NewPermissionRepo(c),
		AuditRepo:      repo.NewAuditEventRepo(c),
		ConfigRepo:     repo.NewPipelineConfigRepo(c),
		OnTaskChanged:  events.record,
	})
	require.NoError(t, err)

	task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug:                "no-tx-branch",
		Title:               "No Tx Branch",
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
		SessionName: "no-tx-branch-session",
	})
	require.NoError(t, err)

	orch.SeedTaskLockForTest(task.ID)

	_, applyErr := orch.ApplyTransitionForTest(ctx, task, sr, pipeline.DoneTransition{})
	require.NoError(t, applyErr)

	require.False(t, orch.TaskLockHeldForTest(task.ID), "taskLocks.Delete must run in the no-tx branch")
	require.True(t, hasEvent(events.all(), task.ID), "OnTaskChanged must fire in the no-tx branch")
}
