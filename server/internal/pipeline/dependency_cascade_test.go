package pipeline_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// makeCascadeTask creates a task at "backlog" stage (non-terminal, non-concept)
// for cascade tests. Backlog is excluded from ListPickable but is a valid
// non-terminal stage for cascade assertions.
func makeCascadeTask(t *testing.T, taskRepo repo.TaskRepo, slug string) string {
	t.Helper()
	ctx := context.Background()
	task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug:                slug,
		Title:               slug,
		Cwd:                 "/tmp",
		CurrentStage:        "backlog",
		Priority:            "medium",
		MaxIterations:       3,
		StageTimeoutSeconds: 1800,
	})
	require.NoError(t, err)
	return task.ID
}

// stageOf is a test helper that reads the current_stage for taskID.
func stageOf(t *testing.T, taskRepo repo.TaskRepo, taskID string) string {
	t.Helper()
	task, err := taskRepo.GetByID(context.Background(), taskID)
	require.NoError(t, err)
	return task.CurrentStage
}

// hasEvent returns true if events contains an entry with the given taskID.
func hasEvent(events []taskChangedEvent, taskID string) bool {
	for _, e := range events {
		if e.taskID == taskID {
			return true
		}
	}
	return false
}

// TestCascade_CancelCancelsDownstreamRecursively verifies that a "cancel" action
// propagates through a two-level chain: upstream → child → grandchild.
func TestCascade_CancelCancelsDownstreamRecursively(t *testing.T) {
	ctx := context.Background()
	orch, taskRepo, depRepo, events := makeTestOrchestratorWithDeps(t)

	upstreamID := makeCascadeTask(t, taskRepo, "cascade-up")
	childID := makeCascadeTask(t, taskRepo, "cascade-child")
	grandchildID := makeCascadeTask(t, taskRepo, "cascade-grandchild")

	// child depends on upstream with action=cancel
	_, err := depRepo.Add(ctx, childID, upstreamID, "done", "cancel")
	require.NoError(t, err)
	// grandchild depends on child with action=cancel
	_, err = depRepo.Add(ctx, grandchildID, childID, "done", "cancel")
	require.NoError(t, err)

	orch.HandleDependentTasksForTest(ctx, upstreamID, "cancelled")

	require.Equal(t, "cancelled", stageOf(t, taskRepo, childID), "child must be cancelled")
	require.Equal(t, "cancelled", stageOf(t, taskRepo, grandchildID), "grandchild must be cancelled recursively")
	require.True(t, hasEvent(events.all(), childID), "OnTaskChanged must fire for child")
	require.True(t, hasEvent(events.all(), grandchildID), "OnTaskChanged must fire for grandchild")
}

// TestCascade_StartActionLeavesDownstreamUnchanged verifies that on_cancel_action="start"
// does not change the downstream stage but does fire OnTaskChanged.
func TestCascade_StartActionLeavesDownstreamUnchanged(t *testing.T) {
	ctx := context.Background()
	orch, taskRepo, depRepo, events := makeTestOrchestratorWithDeps(t)

	upstreamID := makeCascadeTask(t, taskRepo, "start-up")
	downstreamID := makeCascadeTask(t, taskRepo, "start-down")

	_, err := depRepo.Add(ctx, downstreamID, upstreamID, "done", "start")
	require.NoError(t, err)

	orch.HandleDependentTasksForTest(ctx, upstreamID, "cancelled")

	require.Equal(t, "backlog", stageOf(t, taskRepo, downstreamID), "stage must be unchanged for action=start")
	require.True(t, hasEvent(events.all(), downstreamID), "OnTaskChanged must still fire for start action")
}

// TestCascade_OnHoldActionLeavesDownstreamUnchanged verifies that on_cancel_action="on_hold"
// leaves the downstream stage intact.
func TestCascade_OnHoldActionLeavesDownstreamUnchanged(t *testing.T) {
	ctx := context.Background()
	orch, taskRepo, depRepo, events := makeTestOrchestratorWithDeps(t)

	upstreamID := makeCascadeTask(t, taskRepo, "onhold-up")
	downstreamID := makeCascadeTask(t, taskRepo, "onhold-down")

	_, err := depRepo.Add(ctx, downstreamID, upstreamID, "done", "on_hold")
	require.NoError(t, err)

	orch.HandleDependentTasksForTest(ctx, upstreamID, "cancelled")

	require.Equal(t, "backlog", stageOf(t, taskRepo, downstreamID), "stage must be unchanged for action=on_hold")
	require.True(t, hasEvent(events.all(), downstreamID), "OnTaskChanged must fire for on_hold action")
}

// TestCascade_DoneUpstreamDoesNotChangeDownstream verifies that when the upstream
// reaches "done" the downstream stage is not mutated (the lazy picker handles pickup),
// but OnTaskChanged is still fired so the UI refreshes.
func TestCascade_DoneUpstreamDoesNotChangeDownstream(t *testing.T) {
	ctx := context.Background()
	orch, taskRepo, depRepo, events := makeTestOrchestratorWithDeps(t)

	upstreamID := makeCascadeTask(t, taskRepo, "done-up")
	downstreamID := makeCascadeTask(t, taskRepo, "done-down")

	_, err := depRepo.Add(ctx, downstreamID, upstreamID, "done", "on_hold")
	require.NoError(t, err)

	orch.HandleDependentTasksForTest(ctx, upstreamID, "done")

	require.Equal(t, "backlog", stageOf(t, taskRepo, downstreamID), "stage must be unchanged when upstream reaches done")
	require.True(t, hasEvent(events.all(), downstreamID), "OnTaskChanged must fire so the picker can pick up on next tick")
}

// TestCascade_AlreadyTerminalDownstreamSkipped verifies that a downstream task
// already in a terminal stage is not re-processed (no second OnTaskChanged with
// reason "cancelled" for an already-cancelled task).
func TestCascade_AlreadyTerminalDownstreamSkipped(t *testing.T) {
	ctx := context.Background()
	orch, taskRepo, depRepo, events := makeTestOrchestratorWithDeps(t)

	upstreamID := makeCascadeTask(t, taskRepo, "terminal-up")
	downstreamID := makeCascadeTask(t, taskRepo, "terminal-down")

	// put downstream into a terminal state before the cascade
	_, err := taskRepo.Update(ctx, downstreamID, repo.UpdateTaskInput{CurrentStage: ptr("cancelled")})
	require.NoError(t, err)

	_, err = depRepo.Add(ctx, downstreamID, upstreamID, "done", "cancel")
	require.NoError(t, err)

	orch.HandleDependentTasksForTest(ctx, upstreamID, "cancelled")

	// downstream must still be cancelled (not double-processed)
	require.Equal(t, "cancelled", stageOf(t, taskRepo, downstreamID))

	// no "cancelled" reason event must have been fired for the downstream
	for _, ev := range events.all() {
		if ev.taskID == downstreamID {
			require.NotEqual(t, "cancelled", ev.reason,
				"already-terminal downstream must not receive a cascade-cancel OnTaskChanged")
		}
	}
}
