package pipeline_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

// agentStubHandler is a StageHandler test double that claims RequiresAgent()==true.
// Its Execute method returns the preset transition so tests can confirm whether
// the handler was reached (gate did not block) or not (gate blocked).
type agentStubHandler struct {
	stage      string
	transition pipeline.StageTransition
}

func (h *agentStubHandler) Stage() string       { return h.stage }
func (h *agentStubHandler) RequiresAgent() bool { return true }
func (h *agentStubHandler) Execute(_ *pipeline.StageContext) (pipeline.StageTransition, error) {
	return h.transition, nil
}

// openSharedBundle opens an in-memory SQLite DB and registers a test cleanup.
// Use this when a test needs direct repo access alongside the orchestrator.
func openSharedBundle(t *testing.T) *db.DBBundle {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	return bundle
}

// makeOrchFromBundle constructs an orchestrator + TaskRepo backed by the given bundle.
func makeOrchFromBundle(t *testing.T, bundle *db.DBBundle) (*pipeline.PipelineOrchestrator, repo.TaskRepo) {
	t.Helper()
	c := bundle.Client
	orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
		TaskRepo:       repo.NewTaskRepo(c),
		StageRunRepo:   repo.NewStageRunRepo(c),
		PermissionRepo: repo.NewPermissionRepo(c),
		AuditRepo:      repo.NewAuditRepo(c),
		ConfigRepo:     repo.NewPipelineConfigRepo(c),
	})
	require.NoError(t, err)
	return orch, repo.NewTaskRepo(c)
}

func listRunsForTask(t *testing.T, srRepo repo.StageRunRepo, ctx context.Context, taskID string) []*ent.StageRun {
	t.Helper()
	runs, err := srRepo.ListForTask(ctx, taskID)
	require.NoError(t, err)
	return runs
}

// TestOrchestrator_LingeringPendingGate verifies that runProgressTaskLocked returns nil
// and creates no new stage_run when the latest run on the current stage is terminal (done/failed)
// OR is awaiting_user with a dead PID, AND unresolved permission_request rows exist.
// This prevents cascaded respawns after a killed agent leaves pending permission rows behind.
func TestOrchestrator_LingeringPendingGate(t *testing.T) {
	tests := []struct {
		name           string
		latestStatus   string
		pid            *int // nil = no PID column set; nonzero = set PID
		hasPendingReqs bool
		wantBlocked    bool
	}{
		{
			name:           "failed run with pending permission_requests blocks respawn",
			latestStatus:   "failed",
			hasPendingReqs: true,
			wantBlocked:    true,
		},
		{
			name:           "done run with pending permission_requests blocks respawn",
			latestStatus:   "done",
			hasPendingReqs: true,
			wantBlocked:    true,
		},
		{
			name:           "awaiting_user with dead PID and pending requests blocks respawn",
			latestStatus:   "awaiting_user",
			pid:            ptr(999999999), // extremely high PID — never alive in test process
			hasPendingReqs: true,
			wantBlocked:    true,
		},
		{
			name:           "failed run without pending requests allows respawn",
			latestStatus:   "failed",
			hasPendingReqs: false,
			wantBlocked:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			// Shared bundle so orchestrator and direct repo calls see the same DB.
			bundle := openSharedBundle(t)
			orch, taskRepo := makeOrchFromBundle(t, bundle)
			srRepo := repo.NewStageRunRepo(bundle.Client)
			permRepo := repo.NewPermissionRepo(bundle.Client)

			// Install an agent-requiring handler so RequiresAgent()==true triggers the gate.
			// The transition is FailTransition so even if the gate doesn't fire, the test
			// remains deterministic (no real claude spawn).
			orch.SetHandlerOverride("implementation", &agentStubHandler{
				stage:      "implementation",
				transition: pipeline.FailTransition{Reason: "handler reached — gate did not block"},
			})

			task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
				Slug:                "gate-" + tc.name,
				Title:               "Lingering Pending Gate Test",
				Cwd:                 "/tmp",
				CurrentStage:        "implementation",
				Priority:            "medium",
				MaxIterations:       3,
				StageTimeoutSeconds: 1800,
			})
			require.NoError(t, err)

			// Seed a prior stage_run in the requested terminal/zombie status.
			priorRun, err := srRepo.Create(ctx, repo.CreateStageRunInput{
				TaskID:      task.ID,
				Stage:       "implementation",
				Iteration:   0,
				SessionName: "seeded-prior-run",
			})
			require.NoError(t, err)

			runStatus := tc.latestStatus
			updateIn := repo.UpdateStageRunInput{Status: &runStatus}
			if tc.pid != nil {
				updateIn.PID = tc.pid
			}
			priorRun, err = srRepo.Update(ctx, priorRun.ID, updateIn)
			require.NoError(t, err)

			// Optionally attach a pending permission_request (outcome IS NULL = pending).
			if tc.hasPendingReqs {
				_, err = permRepo.CreatePermissionRequest(ctx, repo.CreatePermissionRequestInput{
					StageRunID: priorRun.ID,
					Tool:       "Bash",
				})
				require.NoError(t, err)
			}

			runsBefore := listRunsForTask(t, srRepo, ctx, task.ID)
			countBefore := len(runsBefore)

			result, err := orch.ProgressTask(ctx, task.ID, nil)
			require.NoError(t, err)

			runsAfter := listRunsForTask(t, srRepo, ctx, task.ID)
			countAfter := len(runsAfter)

			if tc.wantBlocked {
				require.Nil(t, result,
					"gate should return nil when latest run is terminal/zombie with unresolved permission_requests")
				require.Equal(t, countBefore, countAfter,
					"no new stage_run row should be created when gate blocks; before=%d after=%d",
					countBefore, countAfter)
			} else {
				// Gate did not fire: handler ran → FailTransition → a new run was created and marked failed.
				require.Greater(t, countAfter, countBefore,
					"gate should not block when there are no pending permission_requests")
			}
		})
	}
}
