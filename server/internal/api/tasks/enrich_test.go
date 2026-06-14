package tasks_test

import (
	"context"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// enrichForTest is a thin wrapper so tests don't repeat the nil permRepo wiring.
func enrichForTest(ctx context.Context, t *testing.T, task *ent.Task, srRepo repo.StageRunRepo, permRepo repo.PermissionRepo) *tasks.EnrichedTask {
	t.Helper()
	enriched, err := tasks.EnrichTask(ctx, task, srRepo, permRepo)
	if err != nil {
		t.Fatalf("EnrichTask: %v", err)
	}
	return enriched
}

func TestEnrichOne_Requeued_PopulatesRetryFields(t *testing.T) {
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })

	taskRepo := repo.NewTaskRepo(bundle.Client)
	srRepo := repo.NewStageRunRepo(bundle.Client)
	permRepo := repo.NewPermissionRepo(bundle.Client)
	ctx := context.Background()

	task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug:         "enrich-requeued",
		Title:        "Enrich Requeued",
		Cwd:          "/tmp/enrich-requeued",
		CurrentStage: "implementation",
		Priority:     "medium",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	sr, err := srRepo.Create(ctx, repo.CreateStageRunInput{
		TaskID:    task.ID,
		Stage:     "implementation",
		Iteration: 1,
	})
	if err != nil {
		t.Fatalf("create stage run: %v", err)
	}

	retryCount := 2
	nextRetryAt := time.Now().Add(30 * time.Second).UTC().Truncate(time.Second)
	status := "requeued"
	_, err = srRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
		Status:      &status,
		RetryCount:  &retryCount,
		NextRetryAt: &nextRetryAt,
	})
	if err != nil {
		t.Fatalf("update stage run: %v", err)
	}

	enriched := enrichForTest(ctx, t, task, srRepo, permRepo)

	if enriched.LatestStageRunStatus == nil || *enriched.LatestStageRunStatus != "requeued" {
		t.Errorf("expected latestStageRunStatus=requeued, got %v", enriched.LatestStageRunStatus)
	}
	if enriched.NeedsUser {
		t.Error("expected needsUser=false for requeued status")
	}
	if enriched.AutoRetryCount == nil || *enriched.AutoRetryCount != 2 {
		t.Errorf("expected autoRetryCount=2, got %v", enriched.AutoRetryCount)
	}
	if enriched.NextRetryAt == nil {
		t.Error("expected nextRetryAt to be set")
	}
}

func TestEnrichOne_RetryCountZero_AutoRetryCountNil(t *testing.T) {
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })

	taskRepo := repo.NewTaskRepo(bundle.Client)
	srRepo := repo.NewStageRunRepo(bundle.Client)
	permRepo := repo.NewPermissionRepo(bundle.Client)
	ctx := context.Background()

	task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug:         "enrich-zero-retry",
		Title:        "Enrich Zero Retry",
		Cwd:          "/tmp/enrich-zero-retry",
		CurrentStage: "implementation",
		Priority:     "medium",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	sr, err := srRepo.Create(ctx, repo.CreateStageRunInput{
		TaskID:    task.ID,
		Stage:     "implementation",
		Iteration: 1,
	})
	if err != nil {
		t.Fatalf("create stage run: %v", err)
	}

	status := "requeued"
	_, err = srRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
		Status: &status,
	})
	if err != nil {
		t.Fatalf("update stage run: %v", err)
	}

	enriched := enrichForTest(ctx, t, task, srRepo, permRepo)

	if enriched.AutoRetryCount != nil {
		t.Errorf("expected autoRetryCount=nil for retry_count=0, got %v", *enriched.AutoRetryCount)
	}
}
