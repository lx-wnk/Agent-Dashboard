package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// TestMaterialize_StampsRoutineID pins the link a "routine" capability grant
// is resolved through: the grant's ContextRef is a schedule id, so a task the
// scheduler materializes has to carry which schedule that was. The reverse
// link (task_schedule.last_task_id) cannot serve — it is overwritten on every
// fire, so it names only the most recent task.
func TestMaterialize_StampsRoutineID(t *testing.T) {
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })

	schedRepo := repo.NewTaskScheduleRepo(bundle.Client, nil)
	sched := mkSchedule(t, schedRepo, repo.CreateTaskScheduleInput{})

	var got NewTaskSpec
	create := func(_ context.Context, spec NewTaskSpec) (string, error) {
		got = spec
		return "task-1", nil
	}

	m := NewMaterializer(create, nil, nil)
	if _, err := m.Materialize(context.Background(), sched, time.Now()); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if got.RoutineID != sched.ID {
		t.Fatalf("RoutineID = %q, want the schedule id %q", got.RoutineID, sched.ID)
	}
}
