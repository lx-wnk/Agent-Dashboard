package tasks_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// seedPendingPermission creates a task with one stage_run and one pending
// permission request, returning the task id, stage_run id, and request id.
func seedPendingPermission(t *testing.T, client *ent.Client, slug string) (taskID, runID, reqID string) {
	t.Helper()
	ctx := context.Background()
	task, err := repo.NewTaskRepo(client).Create(ctx, repo.CreateTaskInput{
		Slug:          slug,
		Title:         slug,
		Cwd:           t.TempDir(),
		MaxIterations: 5,
		Priority:      "normal",
		CurrentStage:  "implementation",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	run, err := repo.NewStageRunRepo(client).Create(ctx, repo.CreateStageRunInput{TaskID: task.ID, Stage: "implementation", Iteration: 0})
	if err != nil {
		t.Fatalf("create stage run: %v", err)
	}
	req, err := repo.NewPermissionRepo(client).CreatePermissionRequest(ctx, repo.CreatePermissionRequestInput{StageRunID: run.ID, Tool: "Bash"})
	if err != nil {
		t.Fatalf("create permission request: %v", err)
	}
	return task.ID, run.ID, req.ID
}

// A caller scoped to task A must not be able to resolve a permission request
// that belongs to task B by passing B's id in the bulk-resolve permissionIds.
func TestBulkResolve_RejectsForeignPermissionIDs(t *testing.T) {
	client, r := newRetryHandler(t, &captureOrchestrator{})
	taskA, _, _ := seedPendingPermission(t, client, "authz-task-a")
	_, runB, reqB := seedPendingPermission(t, client, "authz-task-b")

	body := `{"taskId":"` + taskA + `","outcome":"granted","permissionIds":["` + reqB + `"]}`
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/permission-requests/bulk-resolve", strings.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Resolved int      `json:"resolved"`
		Errors   []string `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Resolved != 0 {
		t.Errorf("foreign permission must not be resolved, got resolved=%d", resp.Resolved)
	}
	if len(resp.Errors) == 0 {
		t.Errorf("expected an error entry for the foreign permission id")
	}
	if n, err := repo.NewPermissionRepo(client).CountForStageRun(context.Background(), runB); err != nil || n != 1 {
		t.Errorf("task B permission must remain pending (count=%d, err=%v)", n, err)
	}
}

// The single-resolve route nests {reqID} under {taskId}; resolving a request
// that belongs to a different task must 404 and leave the request pending.
func TestSingleResolve_RejectsCrossTaskRequest(t *testing.T) {
	client, r := newRetryHandler(t, &captureOrchestrator{})
	taskA, _, _ := seedPendingPermission(t, client, "authz-task-a2")
	_, runB, reqB := seedPendingPermission(t, client, "authz-task-b2")

	url := "/api/tasks/" + taskA + "/permission-requests/" + reqB + "/resolve"
	req := withAuth(t, httptest.NewRequest(http.MethodPost, url, strings.NewReader(`{"outcome":"granted"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-task resolve, got %d: %s", w.Code, w.Body.String())
	}
	if n, err := repo.NewPermissionRepo(client).CountForStageRun(context.Background(), runB); err != nil || n != 1 {
		t.Errorf("task B permission must remain pending (count=%d, err=%v)", n, err)
	}
}
