package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func newSchedule(t *testing.T, r repo.TaskScheduleRepo, name string, in repo.CreateTaskScheduleInput) string {
	t.Helper()
	in.Name = name
	if in.CronExpr == "" {
		in.CronExpr = "0 9 * * *"
	}
	if in.SlugPrefix == "" {
		in.SlugPrefix = "nightly"
	}
	if in.Title == "" {
		in.Title = "Nightly run"
	}
	if in.Cwd == "" {
		in.Cwd = "/tmp"
	}
	if in.MaxIterations == 0 {
		in.MaxIterations = 20
	}
	if in.StageTimeoutSeconds == 0 {
		in.StageTimeoutSeconds = 1800
	}
	s, err := r.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("create schedule %q: %v", name, err)
	}
	return s.ID
}

func TestTaskScheduleRepo_CRUD(t *testing.T) {
	r := repo.NewTaskScheduleRepo(openDB(t))
	ctx := context.Background()

	id := newSchedule(t, r, "daily", repo.CreateTaskScheduleInput{})
	got, err := r.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.CronExpr != "0 9 * * *" || got.Timezone != "UTC" || !got.Enabled {
		t.Fatalf("unexpected defaults: %+v", got)
	}

	newCron := "0 18 * * 1-5"
	updated, err := r.Update(ctx, id, repo.UpdateTaskScheduleInput{CronExpr: &newCron})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.CronExpr != newCron {
		t.Fatalf("cron not updated: %q", updated.CronExpr)
	}

	if _, err := r.SetEnabled(ctx, id, false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	enabled, _ := r.ListEnabled(ctx)
	if len(enabled) != 0 {
		t.Fatalf("expected 0 enabled, got %d", len(enabled))
	}

	if err := r.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := r.GetByID(ctx, id); err == nil {
		t.Fatal("expected not-found after delete")
	}
}

func TestTaskScheduleRepo_ListDue(t *testing.T) {
	r := repo.NewTaskScheduleRepo(openDB(t))
	ctx := context.Background()
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)

	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)

	dueID := newSchedule(t, r, "due", repo.CreateTaskScheduleInput{NextRunAt: &past})
	newSchedule(t, r, "future", repo.CreateTaskScheduleInput{NextRunAt: &future})
	newSchedule(t, r, "no-next", repo.CreateTaskScheduleInput{}) // null next_run_at excluded

	due, err := r.ListDue(ctx, now)
	if err != nil {
		t.Fatalf("ListDue: %v", err)
	}
	if len(due) != 1 || due[0].ID != dueID {
		t.Fatalf("expected only the past schedule due, got %d", len(due))
	}
}

func TestTaskScheduleRepo_UpdateFireState(t *testing.T) {
	r := repo.NewTaskScheduleRepo(openDB(t))
	ctx := context.Background()
	id := newSchedule(t, r, "fire", repo.CreateTaskScheduleInput{})

	fireAt := time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC)
	next := fireAt.Add(24 * time.Hour)
	taskID := "task-123"
	s, err := r.UpdateFireState(ctx, id, repo.FireStateInput{
		LastRunAt:  fireAt,
		LastTaskID: &taskID,
		NextRunAt:  &next,
	})
	if err != nil {
		t.Fatalf("UpdateFireState: %v", err)
	}
	if s.LastTaskID == nil || *s.LastTaskID != taskID {
		t.Fatalf("last_task_id not set: %+v", s.LastTaskID)
	}
	if s.LastRunAt == nil || !s.LastRunAt.Equal(fireAt) {
		t.Fatalf("last_run_at mismatch: %+v", s.LastRunAt)
	}
	if s.NextRunAt == nil || !s.NextRunAt.Equal(next) {
		t.Fatalf("next_run_at mismatch: %+v", s.NextRunAt)
	}
}
