package tasks_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

// requeueCapture records whether RequeueForUser was called and for which task.
type requeueCapture struct {
	called bool
	taskID string
}

func (c *requeueCapture) ProgressTask(_ context.Context, _ string, _ *pipeline.ProgressOpts) (*ent.StageRun, error) {
	return nil, nil
}
func (c *requeueCapture) ResumeFromUser(_ context.Context, _ string, _ string) (*ent.StageRun, error) {
	return nil, nil
}
func (c *requeueCapture) RequeueForUser(_ context.Context, taskID, _ string) (*ent.StageRun, error) {
	c.called = true
	c.taskID = taskID
	return &ent.StageRun{ID: taskID + "-requeued"}, nil
}
func (c *requeueCapture) NotifyTaskTerminated(_ context.Context, _, _ string)      {}
func (c *requeueCapture) InvalidateConfigCache()                                   {}
func (c *requeueCapture) ClearStalePendingPermissions(_ context.Context, _ string) {}

func newApproveAllHandler(t *testing.T, orch tasks.OrchestratorIface) (*ent.Client, *chi.Mux) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	client := bundle.Client
	t.Cleanup(func() { _ = client.Close() })

	h := tasks.NewHandler(tasks.Deps{
		TaskRepo:    repo.NewTaskRepo(client),
		SRRepo:      repo.NewStageRunRepo(client),
		PermRepo:    repo.NewPermissionRepo(client),
		AuditRepo:   repo.NewAuditEventRepo(client),
		CfgRepo:     repo.NewPipelineConfigRepo(client),
		Orchestrator: orch,
		Broadcaster: sse.NewTaskBroadcaster(sse.NewBroadcaster()),
	})
	r := chi.NewRouter()
	r.Use(auth.RequireAuth(testJWTSecret))
	h.Mount(r)
	return client, r
}

// seedAwaitingWithPermissions creates a task whose latest stage run is
// awaiting_user and has n pending permission_requests attached to it.
func seedAwaitingWithPermissions(t *testing.T, client *ent.Client, n int) (taskID, stageRunID string) {
	t.Helper()
	ctx := context.Background()
	taskRepo := repo.NewTaskRepo(client)
	srRepo := repo.NewStageRunRepo(client)
	permRepo := repo.NewPermissionRepo(client)

	task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug:          "approve-pending",
		Title:         "Approve Pending",
		Cwd:           t.TempDir(),
		MaxIterations: 5,
		Priority:      "normal",
		CurrentStage:  "implementation",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	run, err := srRepo.Create(ctx, repo.CreateStageRunInput{
		TaskID:    task.ID,
		Stage:     "implementation",
		Iteration: 0,
	})
	if err != nil {
		t.Fatalf("create stage run: %v", err)
	}
	awaiting := "awaiting_user"
	if _, err := srRepo.Update(ctx, run.ID, repo.UpdateStageRunInput{Status: &awaiting}); err != nil {
		t.Fatalf("update stage run: %v", err)
	}

	for i := range n {
		_, err := permRepo.CreatePermissionRequest(ctx, repo.CreatePermissionRequestInput{
			StageRunID: run.ID,
			Tool:       "Bash",
			Pattern:    strPtr(t, "echo "+string(rune('a'+i))),
		})
		if err != nil {
			t.Fatalf("create perm request: %v", err)
		}
	}
	return task.ID, run.ID
}

func strPtr(_ *testing.T, s string) *string { return &s }

func postApproveAll(t *testing.T, r *chi.Mux, taskID string) *httptest.ResponseRecorder {
	t.Helper()
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/tasks/"+taskID+"/approve-all-pending", nil))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestApproveAllPending_ApprovesAndRequeues verifies the happy path:
// two pending requests → both approved, awaiting_user task → requeued:true.
func TestApproveAllPending_ApprovesAndRequeues(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	orch := &requeueCapture{}
	client, r := newApproveAllHandler(t, orch)
	taskID, runID := seedAwaitingWithPermissions(t, client, 2)
	_ = runID

	w := postApproveAll(t, r, taskID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := resp["approved"].(float64); got != 2 {
		t.Errorf("expected approved=2, got %v", got)
	}
	if !resp["requeued"].(bool) {
		t.Errorf("expected requeued=true")
	}
	if !orch.called || orch.taskID != taskID {
		t.Errorf("expected RequeueForUser called with taskID=%q, called=%v taskID=%q", taskID, orch.called, orch.taskID)
	}

	// Verify requests are now resolved.
	permRepo := repo.NewPermissionRepo(client)
	srRepo := repo.NewStageRunRepo(client)
	runs, _ := srRepo.ListForTask(context.Background(), taskID)
	runIDs := make([]string, len(runs))
	for i, sr := range runs {
		runIDs[i] = sr.ID
	}
	still, _ := permRepo.ListPendingForTask(context.Background(), taskID, runIDs)
	if len(still) != 0 {
		t.Errorf("expected 0 pending after approve-all, got %d", len(still))
	}
}

// TestApproveAllPending_NoPendingPerms verifies the handler returns approved=0
// and requeued=false when there are no pending permission requests.
func TestApproveAllPending_NoPendingPerms(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	orch := &requeueCapture{}
	client, r := newApproveAllHandler(t, orch)

	// Seed a task with no permission requests.
	taskID, _ := seedAwaitingWithPermissions(t, client, 0)

	w := postApproveAll(t, r, taskID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := resp["approved"].(float64); got != 0 {
		t.Errorf("expected approved=0, got %v", got)
	}
	if resp["requeued"].(bool) {
		t.Errorf("expected requeued=false when no pending perms")
	}
}

// TestApproveAllPending_UnknownTask returns 404.
func TestApproveAllPending_UnknownTask(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	_, r := newApproveAllHandler(t, &requeueCapture{})
	w := postApproveAll(t, r, "nonexistent-task-id")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
