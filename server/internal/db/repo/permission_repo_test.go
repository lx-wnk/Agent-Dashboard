package repo_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/stretchr/testify/require"
)

func TestPermissionRepo_CreateAndListTaskPermission(t *testing.T) {
	client := openDB(t)
	ctx := context.Background()
	tr := repo.NewTaskRepo(client)
	pr := repo.NewPermissionRepo(client)

	taskID := createTask(t, tr, "perm-task-1")

	perm, err := pr.CreateTaskPermission(ctx, repo.CreateTaskPermissionInput{
		TaskID:  taskID,
		Tool:    "Bash",
		Granted: true,
	})
	require.NoError(t, err)
	require.Equal(t, "Bash", perm.Tool)

	perms, err := pr.ListTaskPermissions(ctx, taskID)
	require.NoError(t, err)
	require.Len(t, perms, 1)
	require.Equal(t, perm.ID, perms[0].ID)
}

func TestPermissionRepo_DeleteTaskPermission(t *testing.T) {
	client := openDB(t)
	ctx := context.Background()
	tr := repo.NewTaskRepo(client)
	pr := repo.NewPermissionRepo(client)

	taskID := createTask(t, tr, "perm-task-2")

	perm, err := pr.CreateTaskPermission(ctx, repo.CreateTaskPermissionInput{
		TaskID: taskID, Tool: "Read", Granted: true,
	})
	require.NoError(t, err)

	err = pr.DeleteTaskPermission(ctx, perm.ID)
	require.NoError(t, err)

	perms, err := pr.ListTaskPermissions(ctx, taskID)
	require.NoError(t, err)
	require.Empty(t, perms)
}

func createStageRun(t *testing.T, sr repo.StageRunRepo, taskID string) string {
	t.Helper()
	run, err := sr.Create(context.Background(), repo.CreateStageRunInput{
		TaskID: taskID, Stage: "concept", Iteration: 1,
	})
	if err != nil {
		t.Fatalf("create stage run: %v", err)
	}
	return run.ID
}

func TestPermissionRepo_CreatePermissionRequest(t *testing.T) {
	client := openDB(t)
	ctx := context.Background()
	tr := repo.NewTaskRepo(client)
	sr := repo.NewStageRunRepo(client)
	pr := repo.NewPermissionRepo(client)

	taskID := createTask(t, tr, "perm-task-3")
	runID := createStageRun(t, sr, taskID)

	reason := "need bash"
	req, err := pr.CreatePermissionRequest(ctx, repo.CreatePermissionRequestInput{
		StageRunID: runID, Tool: "Bash", Reason: &reason,
	})
	require.NoError(t, err)
	require.Equal(t, "Bash", req.Tool)
	require.Nil(t, req.Outcome)

	got, err := pr.GetPermissionRequest(ctx, req.ID)
	require.NoError(t, err)
	require.Equal(t, req.ID, got.ID)
}

func TestPermissionRepo_ResolvePermissionRequest(t *testing.T) {
	client := openDB(t)
	ctx := context.Background()
	tr := repo.NewTaskRepo(client)
	sr := repo.NewStageRunRepo(client)
	pr := repo.NewPermissionRepo(client)

	taskID := createTask(t, tr, "perm-task-4")
	runID := createStageRun(t, sr, taskID)

	req, err := pr.CreatePermissionRequest(ctx, repo.CreatePermissionRequestInput{
		StageRunID: runID, Tool: "Bash",
	})
	require.NoError(t, err)

	err = pr.ResolvePermissionRequest(ctx, req.ID, "granted")
	require.NoError(t, err)

	got, err := pr.GetPermissionRequest(ctx, req.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Outcome)
	require.Equal(t, "granted", *got.Outcome)
}

func TestPermissionRepo_ResolvePermissionRequest_Idempotent(t *testing.T) {
	client := openDB(t)
	ctx := context.Background()
	tr := repo.NewTaskRepo(client)
	sr := repo.NewStageRunRepo(client)
	pr := repo.NewPermissionRepo(client)

	taskID := createTask(t, tr, "perm-task-5")
	runID := createStageRun(t, sr, taskID)

	req, err := pr.CreatePermissionRequest(ctx, repo.CreatePermissionRequestInput{
		StageRunID: runID, Tool: "Write",
	})
	require.NoError(t, err)

	err = pr.ResolvePermissionRequest(ctx, req.ID, "granted")
	require.NoError(t, err)

	// Second resolve must return an error — double-resolution is not allowed.
	err = pr.ResolvePermissionRequest(ctx, req.ID, "denied")
	require.Error(t, err)
}

func TestPermissionRepo_ListPending(t *testing.T) {
	client := openDB(t)
	ctx := context.Background()
	tr := repo.NewTaskRepo(client)
	sr := repo.NewStageRunRepo(client)
	pr := repo.NewPermissionRepo(client)

	taskID := createTask(t, tr, "perm-task-6")
	runID := createStageRun(t, sr, taskID)

	req1, err := pr.CreatePermissionRequest(ctx, repo.CreatePermissionRequestInput{
		StageRunID: runID, Tool: "Bash",
	})
	require.NoError(t, err)
	req2, err := pr.CreatePermissionRequest(ctx, repo.CreatePermissionRequestInput{
		StageRunID: runID, Tool: "Read",
	})
	require.NoError(t, err)
	_ = req2

	err = pr.ResolvePermissionRequest(ctx, req1.ID, "granted")
	require.NoError(t, err)

	pending, err := pr.ListPendingForStageRun(ctx, runID)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, "Read", pending[0].Tool)
}

func TestPermissionRepo_ExpirePendingForStageRun(t *testing.T) {
	client := openDB(t)
	ctx := context.Background()
	tr := repo.NewTaskRepo(client)
	sr := repo.NewStageRunRepo(client)
	pr := repo.NewPermissionRepo(client)

	taskID := createTask(t, tr, "perm-task-7")
	runID := createStageRun(t, sr, taskID)

	// Two requests: one granted, one still pending.
	granted, err := pr.CreatePermissionRequest(ctx, repo.CreatePermissionRequestInput{
		StageRunID: runID, Tool: "Bash",
	})
	require.NoError(t, err)
	_, err = pr.CreatePermissionRequest(ctx, repo.CreatePermissionRequestInput{
		StageRunID: runID, Tool: "Read",
	})
	require.NoError(t, err)

	require.NoError(t, pr.ResolvePermissionRequest(ctx, granted.ID, "granted"))

	// ExpirePending should touch exactly the one remaining pending request.
	n, err := pr.ExpirePendingForStageRun(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	// The granted request must keep its original outcome.
	g, err := pr.GetPermissionRequest(ctx, granted.ID)
	require.NoError(t, err)
	require.NotNil(t, g.Outcome)
	require.Equal(t, "granted", *g.Outcome)

	// No pending requests should remain.
	count, err := pr.CountForStageRun(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, 0, count)

	// Calling again when nothing is pending is a harmless no-op (returns 0, nil).
	n2, err := pr.ExpirePendingForStageRun(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, 0, n2)
}
