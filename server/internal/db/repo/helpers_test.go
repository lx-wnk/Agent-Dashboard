package repo_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func openDB(t *testing.T) *ent.Client {
	t.Helper()
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	return bundle.Client
}

func createTask(t *testing.T, r repo.TaskRepo, slug string) string {
	t.Helper()
	task, err := r.Create(context.Background(), repo.CreateTaskInput{
		Slug:                slug,
		Title:               "Test Task",
		Cwd:                 "/tmp",
		CurrentStage:        "concept",
		Priority:            "medium",
		MaxIterations:       20,
		StageTimeoutSeconds: 1800,
	})
	if err != nil {
		t.Fatalf("create task %q: %v", slug, err)
	}
	return task.ID
}
