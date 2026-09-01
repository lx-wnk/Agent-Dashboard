package tasks_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// decodeRows decodes a JSON array response into raw maps so assertions run
// against the actual wire keys. Decoding into a typed struct would pass
// regardless of the key casing the server emits.
func decodeRows(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("unmarshal response: %v (body: %s)", err, body)
	}
	return rows
}

func assertKeys(t *testing.T, row map[string]any, want, forbidden []string) {
	t.Helper()
	for _, k := range want {
		if _, ok := row[k]; !ok {
			t.Errorf("missing key %q in %v", k, row)
		}
	}
	for _, k := range forbidden {
		if _, ok := row[k]; ok {
			t.Errorf("unexpected key %q in %v", k, row)
		}
	}
}

// TestListPermissions_WireFormat asserts the camelCase keys the client declares
// in src/types.ts, and that no snake_case entity key leaks onto the wire.
func TestListPermissions_WireFormat(t *testing.T) {
	client, r := newTestHandlerWithRepos(t)
	task, err := repo.NewTaskRepo(client).Create(testCtx(t), repo.CreateTaskInput{
		Slug:          "perm-wire-format",
		Title:         "Permission Wire Format",
		Cwd:           t.TempDir(),
		MaxIterations: 5,
		Priority:      "normal",
		CurrentStage:  "implementation",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	permRepo := repo.NewPermissionRepo(client)
	pattern := "npm run *"
	if _, err = permRepo.CreateTaskPermission(testCtx(t), repo.CreateTaskPermissionInput{
		TaskID:    task.ID,
		Tool:      "Bash",
		Pattern:   &pattern,
		Granted:   true,
		DecidedBy: "user-1",
	}); err != nil {
		t.Fatalf("create granted permission: %v", err)
	}
	// Zero values: the entity's omitempty tags would drop granted and pattern
	// from the payload entirely instead of sending false and null.
	if _, err = permRepo.CreateTaskPermission(testCtx(t), repo.CreateTaskPermissionInput{
		TaskID:  task.ID,
		Tool:    "Write",
		Granted: false,
	}); err != nil {
		t.Fatalf("create denied permission: %v", err)
	}

	req := withAuth(t, httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/permissions", nil))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	rows := decodeRows(t, w.Body.Bytes())
	if len(rows) != 2 {
		t.Fatalf("expected 2 permissions, got %d: %s", len(rows), w.Body.String())
	}

	byTool := map[string]map[string]any{}
	for _, row := range rows {
		tool, _ := row["tool"].(string)
		byTool[tool] = row
	}

	want := []string{"id", "taskId", "tool", "pattern", "granted", "decidedBy", "requestedAt", "decidedAt", "expiresAt"}
	forbidden := []string{"task_id", "requested_at", "decided_at", "decided_by", "expires_at", "manual_override", "edges"}
	for tool, row := range byTool {
		t.Run(tool, func(t *testing.T) {
			assertKeys(t, row, want, forbidden)
		})
	}

	granted := byTool["Bash"]
	if granted["taskId"] != task.ID {
		t.Errorf("taskId = %v, want %q", granted["taskId"], task.ID)
	}
	if granted["decidedBy"] != "user-1" {
		t.Errorf("decidedBy = %v, want %q", granted["decidedBy"], "user-1")
	}
	if granted["granted"] != true {
		t.Errorf("granted = %v, want true", granted["granted"])
	}
	if granted["requestedAt"] == "" || granted["requestedAt"] == nil {
		t.Errorf("requestedAt = %v, want an RFC3339 timestamp", granted["requestedAt"])
	}
	if granted["decidedAt"] == nil {
		t.Error("decidedAt = null, want a timestamp for a decided grant")
	}
	if granted["expiresAt"] != nil {
		t.Errorf("expiresAt = %v, want null", granted["expiresAt"])
	}

	denied := byTool["Write"]
	if denied["granted"] != false {
		t.Errorf("granted = %v, want false (not omitted)", denied["granted"])
	}
	if denied["pattern"] != nil {
		t.Errorf("pattern = %v, want null (not omitted)", denied["pattern"])
	}
	if denied["decidedBy"] != nil {
		t.Errorf("decidedBy = %v, want null", denied["decidedBy"])
	}
}

// TestGrantPermission_WireFormat asserts the single-grant response uses the same
// camelCase shape as the list, since the client types both as TaskPermission.
func TestGrantPermission_WireFormat(t *testing.T) {
	client, r := newTestHandlerWithRepos(t)
	task, err := repo.NewTaskRepo(client).Create(testCtx(t), repo.CreateTaskInput{
		Slug:          "grant-wire-format",
		Title:         "Grant Wire Format",
		Cwd:           t.TempDir(),
		MaxIterations: 5,
		Priority:      "normal",
		CurrentStage:  "implementation",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID+"/permissions", strings.NewReader(`{"tool":"Write"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var row map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &row); err != nil {
		t.Fatalf("unmarshal response: %v (body: %s)", err, w.Body.String())
	}
	assertKeys(t, row,
		[]string{"id", "taskId", "tool", "pattern", "granted", "decidedBy", "requestedAt", "decidedAt", "expiresAt"},
		[]string{"task_id", "requested_at", "decided_at", "decided_by", "expires_at", "manual_override", "edges"},
	)
}

// TestListAudit_WireFormat asserts the audit rows carry the keys AuditLogTab.vue
// reads (timestamp, details, actor), not the storage column names.
func TestListAudit_WireFormat(t *testing.T) {
	client, r := newTestHandlerWithRepos(t)
	task, err := repo.NewTaskRepo(client).Create(testCtx(t), repo.CreateTaskInput{
		Slug:          "audit-wire-format",
		Title:         "Audit Wire Format",
		Cwd:           t.TempDir(),
		MaxIterations: 5,
		Priority:      "normal",
		CurrentStage:  "implementation",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	auditRepo := repo.NewAuditEventRepo(client)
	if err = auditRepo.RecordTaskAudit(testCtx(t), task.ID, nil, "retry_requested", "task:"+task.ID,
		map[string]any{"actor": "user", "stage": "implementation"}); err != nil {
		t.Fatalf("record tagged audit: %v", err)
	}
	if err = auditRepo.RecordTaskAudit(testCtx(t), task.ID, nil, "stage_transition", "task:"+task.ID, nil); err != nil {
		t.Fatalf("record untagged audit: %v", err)
	}

	req := withAuth(t, httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/audit", nil))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	rows := decodeRows(t, w.Body.Bytes())
	if len(rows) != 2 {
		t.Fatalf("expected 2 audit entries, got %d: %s", len(rows), w.Body.String())
	}

	byAction := map[string]map[string]any{}
	for _, row := range rows {
		assertKeys(t, row,
			[]string{"id", "taskId", "userId", "actor", "action", "target", "timestamp", "details"},
			[]string{"task_id", "user_id", "ts", "metadata", "edges"},
		)
		action, _ := row["action"].(string)
		byAction[action] = row
	}

	tagged := byAction["retry_requested"]
	if tagged["actor"] != "user" {
		t.Errorf("actor = %v, want %q from metadata", tagged["actor"], "user")
	}
	if tagged["taskId"] != task.ID {
		t.Errorf("taskId = %v, want %q", tagged["taskId"], task.ID)
	}
	if _, ok := tagged["details"].(map[string]any); !ok {
		t.Errorf("details = %v, want the metadata object", tagged["details"])
	}
	if tagged["timestamp"] == "" || tagged["timestamp"] == nil {
		t.Errorf("timestamp = %v, want an RFC3339 timestamp", tagged["timestamp"])
	}

	untagged := byAction["stage_transition"]
	if untagged["actor"] != "system" {
		t.Errorf("actor = %v, want %q for a write with no actor tag and no user", untagged["actor"], "system")
	}
	if untagged["details"] != nil {
		t.Errorf("details = %v, want null", untagged["details"])
	}
}

// TestListStageRuns_WireFormat asserts the camelCase keys src/types.ts declares
// for StageRun, and that a zero iteration/cost/token count is sent as 0 rather
// than dropped by the entity's omitempty tags.
func TestListStageRuns_WireFormat(t *testing.T) {
	client, r := newTestHandlerWithRepos(t)
	task, err := repo.NewTaskRepo(client).Create(testCtx(t), repo.CreateTaskInput{
		Slug:          "stage-run-wire-format",
		Title:         "Stage Run Wire Format",
		Cwd:           t.TempDir(),
		MaxIterations: 5,
		Priority:      "normal",
		CurrentStage:  "implementation",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	srRepo := repo.NewStageRunRepo(client)
	finished, err := srRepo.Create(testCtx(t), repo.CreateStageRunInput{
		TaskID:      task.ID,
		Stage:       "implementation",
		Iteration:   1,
		SessionName: "impl-session",
	})
	if err != nil {
		t.Fatalf("create finished run: %v", err)
	}
	started := time.Now().Add(-time.Hour)
	ended := time.Now()
	done := "done"
	sessionID := "sess-1"
	tokens := 1234
	cost := 42
	if _, err = srRepo.Update(testCtx(t), finished.ID, repo.UpdateStageRunInput{
		Status:     &done,
		SessionID:  &sessionID,
		StartedAt:  &started,
		EndedAt:    &ended,
		TokensUsed: &tokens,
		CostCents:  &cost,
	}); err != nil {
		t.Fatalf("update finished run: %v", err)
	}
	// Zero values: iteration, tokensUsed and costCents are all 0 here, which the
	// entity's omitempty tags drop from the payload entirely.
	if _, err = srRepo.Create(testCtx(t), repo.CreateStageRunInput{
		TaskID:    task.ID,
		Stage:     "self_review",
		Iteration: 0,
	}); err != nil {
		t.Fatalf("create pending run: %v", err)
	}

	req := withAuth(t, httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/stage-runs", nil))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	rows := decodeRows(t, w.Body.Bytes())
	if len(rows) != 2 {
		t.Fatalf("expected 2 stage runs, got %d: %s", len(rows), w.Body.String())
	}

	byStage := map[string]map[string]any{}
	want := []string{"id", "taskId", "stage", "sessionId", "sessionName", "pid", "status", "iteration", "output", "tokensUsed", "costCents", "startedAt", "endedAt", "lastGrantAt"}
	forbidden := []string{"task_id", "session_id", "session_name", "started_at", "ended_at", "tokens_used", "cost_cents", "last_grant_at", "created_at", "retry_count", "next_retry_at", "pending_user_prompt", "edges"}
	for _, row := range rows {
		stage, _ := row["stage"].(string)
		byStage[stage] = row
		t.Run(stage, func(t *testing.T) {
			assertKeys(t, row, want, forbidden)
		})
	}

	impl := byStage["implementation"]
	if impl["taskId"] != task.ID {
		t.Errorf("taskId = %v, want %q", impl["taskId"], task.ID)
	}
	if impl["sessionName"] != "impl-session" {
		t.Errorf("sessionName = %v, want %q", impl["sessionName"], "impl-session")
	}
	if impl["startedAt"] == nil {
		t.Error("startedAt = null, want a timestamp for a run that started")
	}
	if impl["endedAt"] == nil {
		t.Error("endedAt = null, want a timestamp for a finished run")
	}
	if impl["tokensUsed"] != float64(1234) {
		t.Errorf("tokensUsed = %v, want 1234", impl["tokensUsed"])
	}

	pending := byStage["self_review"]
	if pending["iteration"] != float64(0) {
		t.Errorf("iteration = %v, want 0 (not omitted)", pending["iteration"])
	}
	if pending["costCents"] != float64(0) {
		t.Errorf("costCents = %v, want 0 (not omitted)", pending["costCents"])
	}
	if pending["startedAt"] != nil {
		t.Errorf("startedAt = %v, want null", pending["startedAt"])
	}
	if pending["output"] != nil {
		t.Errorf("output = %v, want null (not omitted)", pending["output"])
	}
}
