package repo_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/stretchr/testify/require"
)

func TestTaskRepo_CreateAndGet(t *testing.T) {
	client, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

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
	client, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

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
	client, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

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
