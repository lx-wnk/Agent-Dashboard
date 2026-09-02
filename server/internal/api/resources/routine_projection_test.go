package resources_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	apiresources "github.com/lx-wnk/agent-dashboard/server/internal/api/resources"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/memory"
)

// newRoutineMux is newUngrantedMux's shape plus the schedule repo the routine
// projection reads, returned so a test can create schedules.
func newRoutineMux(t *testing.T) (*chi.Mux, repo.TaskScheduleRepo, context.Context) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	ctx := context.Background()
	repo.SeedCapabilities(ctx, repo.NewCapabilityRepo(bundle.Client))
	schedRepo := repo.NewTaskScheduleRepo(bundle.Client)

	mux := chi.NewRouter()
	apiresources.NewHandler(repo.NewResourceRepo(bundle.Client), schedRepo, memory.Gate{
		Capabilities: repo.NewCapabilityRepo(bundle.Client),
		Grants:       repo.NewGrantRepo(bundle.Client),
		GrantUsage:   repo.NewGrantUsageRepo(bundle.Client, bundle.WriteClient),
	}, true).Mount(mux)
	return mux, schedRepo, ctx
}

func mkSchedule(t *testing.T, r repo.TaskScheduleRepo, name string, enabled bool) string {
	t.Helper()
	s, err := r.Create(context.Background(), repo.CreateTaskScheduleInput{
		Name:                name,
		CronExpr:            "0 9 * * *",
		SlugPrefix:          name,
		Title:               name,
		Cwd:                 "/tmp",
		MaxIterations:       20,
		StageTimeoutSeconds: 1800,
	})
	require.NoError(t, err)
	if !enabled {
		_, err = r.SetEnabled(context.Background(), s.ID, false)
		require.NoError(t, err)
	}
	return s.ID
}

// TestList_RoutineProjectsSchedules is the fix for "ResourceKindRoutine is a
// constant with no writer": the registry table still holds no routine rows and
// deliberately never will, because task_schedule already is the routine. The
// id the projection reports is the one a routine grant is anchored to, so this
// test pins it against the schedule id rather than any surrogate.
func TestList_RoutineProjectsSchedules(t *testing.T) {
	mux, schedules, _ := newRoutineMux(t)
	id := mkSchedule(t, schedules, "nightly", true)

	w := get(t, mux, "/api/resources?kind=routine")
	require.Equal(t, http.StatusOK, w.Code)

	rows := decodeList(t, w)
	require.Len(t, rows, 1)
	require.Equal(t, id, rows[0]["id"], "the reported id must be the schedule id a routine grant names")
	require.Equal(t, repo.ResourceKindRoutine, rows[0]["kind"])
	require.Equal(t, "nightly", rows[0]["name"])
	require.Equal(t, repo.ResourceStateEnabled, rows[0]["state"])
	require.Equal(t, string(repo.ScopeGlobal), rows[0]["scopeKind"])
}

// TestList_RoutineReportsDisabledState keeps the two schedule states
// distinguishable in the registry view. Without it a paused routine would read
// as an active one, which is exactly the class of "two states, one rendering"
// bug the five-state settings spec exists to prevent.
func TestList_RoutineReportsDisabledState(t *testing.T) {
	mux, schedules, _ := newRoutineMux(t)
	mkSchedule(t, schedules, "paused", false)

	rows := decodeList(t, get(t, mux, "/api/resources?kind=routine"))
	require.Len(t, rows, 1)
	require.Equal(t, repo.ResourceStateDisabled, rows[0]["state"])
}

// TestList_RoutineWithNoSchedulesIsAnEmptyArray is the projection's half of
// TestList_EmptyKindIsAnEmptyArrayNotNull: a nil slice encodes as null and
// would crash a client that maps over it.
func TestList_RoutineWithNoSchedulesIsAnEmptyArray(t *testing.T) {
	mux, _, _ := newRoutineMux(t)

	w := get(t, mux, "/api/resources?kind=routine")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "[]\n", w.Body.String())
}
