package repo_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/stretchr/testify/require"
)

func TestTaskRepo_CreateAndGet(t *testing.T) {
	client := openDB(t)

	r := repo.NewTaskRepo(client)
	ctx := context.Background()

	desc := "fix the login"
	task, err := r.Create(ctx, repo.CreateTaskInput{
		Slug:                "fix-login",
		Title:               "Fix Login",
		Description:         &desc,
		Cwd:                 "/tmp/proj",
		CurrentStage:        "concept",
		Priority:            "medium",
		MaxIterations:       20,
		StageTimeoutSeconds: 1800,
	})
	require.NoError(t, err)
	require.Equal(t, "fix-login", task.Slug)

	got, err := r.GetByID(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, task.ID, got.ID)

	_, err = r.GetBySlug(ctx, "fix-login")
	require.NoError(t, err)
}

func TestTaskRepo_Update_CurrentStage(t *testing.T) {
	client := openDB(t)

	r := repo.NewTaskRepo(client)
	ctx := context.Background()

	task, err := r.Create(ctx, repo.CreateTaskInput{
		Slug: "my-task", Title: "My Task", Cwd: "/tmp",
		CurrentStage: "concept", Priority: "medium",
		MaxIterations: 20, StageTimeoutSeconds: 1800,
	})
	require.NoError(t, err)

	stage := "implementation"
	updated, err := r.Update(ctx, task.ID, repo.UpdateTaskInput{CurrentStage: &stage})
	require.NoError(t, err)
	require.Equal(t, "implementation", updated.CurrentStage)
}

func TestTaskRepo_Delete(t *testing.T) {
	client := openDB(t)

	r := repo.NewTaskRepo(client)
	ctx := context.Background()

	task, err := r.Create(ctx, repo.CreateTaskInput{
		Slug: "to-delete", Title: "Delete Me", Cwd: "/tmp",
		CurrentStage: "concept", Priority: "medium",
		MaxIterations: 20, StageTimeoutSeconds: 1800,
	})
	require.NoError(t, err)

	err = r.Delete(ctx, task.ID)
	require.NoError(t, err)

	_, err = r.GetByID(ctx, task.ID)
	require.Error(t, err)
}

func TestTaskRepo_Update_MetadataClear(t *testing.T) {
	client := openDB(t)
	r := repo.NewTaskRepo(client)
	ctx := context.Background()

	meta := map[string]any{"key": "value"}
	task, err := r.Create(ctx, repo.CreateTaskInput{
		Slug: "meta-task", Title: "Meta Task", Cwd: "/tmp",
		CurrentStage: "concept", Priority: "medium",
		MaxIterations: 20, StageTimeoutSeconds: 1800,
		Metadata: meta,
	})
	require.NoError(t, err)
	require.NotNil(t, task.Metadata)

	updated, err := r.Update(ctx, task.ID, repo.UpdateTaskInput{MetadataClear: true})
	require.NoError(t, err)
	require.Nil(t, updated.Metadata)
}

func TestTaskRepo_ListForUser_AdminSeesAll(t *testing.T) {
	client := openDB(t)
	r := repo.NewTaskRepo(client)
	ctx := context.Background()

	uid1 := "user-1"
	uid2 := "user-2"
	_, err := r.Create(ctx, repo.CreateTaskInput{
		Slug: "task-u1", Title: "Task U1", Cwd: "/tmp",
		CurrentStage: "concept", Priority: "medium",
		MaxIterations: 20, StageTimeoutSeconds: 1800,
		UserID: &uid1,
	})
	require.NoError(t, err)
	_, err = r.Create(ctx, repo.CreateTaskInput{
		Slug: "task-u2", Title: "Task U2", Cwd: "/tmp",
		CurrentStage: "concept", Priority: "medium",
		MaxIterations: 20, StageTimeoutSeconds: 1800,
		UserID: &uid2,
	})
	require.NoError(t, err)

	// Non-admin: only own tasks.
	own, err := r.ListForUser(ctx, uid1, false)
	require.NoError(t, err)
	require.Len(t, own, 1)
	require.Equal(t, "task-u1", own[0].Slug)

	// Admin: all tasks.
	all, err := r.ListForUser(ctx, uid1, true)
	require.NoError(t, err)
	require.Len(t, all, 2)
}

func TestTaskRepo_ListPickable(t *testing.T) {
	client := openDB(t)
	r := repo.NewTaskRepo(client)
	ctx := context.Background()

	stages := []struct {
		slug      string
		stage     string
		pickable  bool
	}{
		{"t-implementation", "implementation", true},
		{"t-done", "done", false},
		{"t-cancelled", "cancelled", false},
		{"t-on-hold", "on_hold", false},
		{"t-concept", "concept", false},
	}

	for _, tc := range stages {
		_, err := r.Create(ctx, repo.CreateTaskInput{
			Slug: tc.slug, Title: tc.slug, Cwd: "/tmp",
			CurrentStage: tc.stage, Priority: "medium",
			MaxIterations: 20, StageTimeoutSeconds: 1800,
		})
		require.NoError(t, err)
	}

	pickable, err := r.ListPickable(ctx)
	require.NoError(t, err)
	require.Len(t, pickable, 1)
	require.Equal(t, "t-implementation", pickable[0].Slug)
}

func TestTaskRepo_GetBySlug_NotFound(t *testing.T) {
	client := openDB(t)
	r := repo.NewTaskRepo(client)
	ctx := context.Background()

	_, err := r.GetBySlug(ctx, "does-not-exist")
	require.Error(t, err)
}
