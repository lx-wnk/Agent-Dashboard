package tasks_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func TestCreatePermissionRequest_AllowAll_ApplicationToolStaysPending(t *testing.T) {
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	client := bundle.Client
	t.Cleanup(func() { _ = client.Close() })
	_, r := newTestHandlerWithBroadcaster(t, client)

	task, err := repo.NewTaskRepo(client).Create(testCtx(t), repo.CreateTaskInput{
		Slug: "app-tool-pending", Title: "App tool", Cwd: "/tmp/at", CurrentStage: "implementation", Priority: "medium",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err = client.Task.UpdateOneID(task.ID).SetAutonomy("full").Save(testCtx(t)); err != nil {
		t.Fatalf("set autonomy: %v", err)
	}
	sr, err := repo.NewStageRunRepo(client).Create(testCtx(t), repo.CreateStageRunInput{TaskID: task.ID, Stage: "implementation", Iteration: 1})
	if err != nil {
		t.Fatalf("create stage run: %v", err)
	}

	b, _ := json.Marshal(map[string]any{"stageRunId": sr.ID, "tool": "mcp__mail__imap_move_email"})
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/permission-requests", bytes.NewReader(b)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var result map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &result)
	if result["outcome"] == "granted" {
		t.Fatal("an application tool must never be auto-approved, whatever the task's autonomy")
	}
}

func TestBulkCreatePermissionRequests_AllowAll_ApplicationToolStaysPending(t *testing.T) {
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	client := bundle.Client
	t.Cleanup(func() { _ = client.Close() })
	_, r := newTestHandlerWithBroadcaster(t, client)

	task, err := repo.NewTaskRepo(client).Create(testCtx(t), repo.CreateTaskInput{
		Slug: "app-tool-bulk-pending", Title: "App tool bulk", Cwd: "/tmp/atb", CurrentStage: "implementation", Priority: "medium",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err = client.Task.UpdateOneID(task.ID).SetAutonomy("full").Save(testCtx(t)); err != nil {
		t.Fatalf("set autonomy: %v", err)
	}
	sr, err := repo.NewStageRunRepo(client).Create(testCtx(t), repo.CreateStageRunInput{TaskID: task.ID, Stage: "implementation", Iteration: 1})
	if err != nil {
		t.Fatalf("create stage run: %v", err)
	}

	b, _ := json.Marshal(map[string]any{
		"stageRunId": sr.ID,
		"entries": []map[string]any{
			{"tool": "mcp__mail__imap_move_email"},
			{"tool": "Read"},
		},
	})
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/permission-requests/bulk", bytes.NewReader(b)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var results []struct {
		Tool        string `json:"tool"`
		AutoGranted bool   `json:"autoGranted"`
		RequestID   string `json:"requestId"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &results); err != nil {
		t.Fatalf("unmarshal: %v: %s", err, rr.Body.String())
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, res := range results {
		switch res.Tool {
		case "mcp__mail__imap_move_email":
			if res.AutoGranted {
				t.Fatal("an application tool must never be auto-approved, whatever the task's autonomy")
			}
			if res.RequestID == "" {
				t.Fatal("expected a requestId for the pending application tool")
			}
		case "Read":
			if !res.AutoGranted {
				t.Fatal("a non-application tool must still be auto-approved for an allow-all task")
			}
		default:
			t.Fatalf("unexpected tool in results: %s", res.Tool)
		}
	}
}
