package tasks_test

import (
	"bytes"
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

// testCtx returns a background context for use in tests.
func testCtx(_ *testing.T) context.Context {
	return context.Background()
}

const testJWTSecret = "test-secret-for-tasks"

// withAuth adds a valid JWT cookie to the request so RequireAuth passes.
func withAuth(t *testing.T, r *http.Request) *http.Request {
	t.Helper()
	token, err := auth.SignJWT(auth.JWTPayload{Sub: "user-1", Login: "testuser", IsAdmin: true}, testJWTSecret, 3600)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	r.AddCookie(&http.Cookie{Name: "auth_token", Value: token})
	return r
}

// newTestHandler creates a Handler backed by an in-memory SQLite DB.
func newTestHandler(t *testing.T) (*tasks.Handler, *chi.Mux) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	client := bundle.Client
	t.Cleanup(func() { _ = client.Close() })

	taskRepo := repo.NewTaskRepo(client)
	srRepo := repo.NewStageRunRepo(client)
	permRepo := repo.NewPermissionRepo(client)
	auditRepo := repo.NewAuditEventRepo(client)
	cfgRepo := repo.NewPipelineConfigRepo(client)

	broadcaster := sse.NewTaskBroadcaster(sse.NewBroadcaster())

	h := tasks.NewHandler(tasks.Deps{
		TaskRepo:    taskRepo,
		SRRepo:      srRepo,
		PermRepo:    permRepo,
		AuditRepo:   auditRepo,
		CfgRepo:     cfgRepo,
		Orchestrator: &noopOrchestrator{},
		Broadcaster: broadcaster,
	})

	r := chi.NewRouter()
	r.Use(auth.RequireAuth(testJWTSecret))
	h.Mount(r)
	return h, r
}

// newTestHandlerWithBroadcaster creates a Handler against the provided client
// and returns its TaskBroadcaster so tests can subscribe to emitted events.
func newTestHandlerWithBroadcaster(t *testing.T, client *ent.Client) (*sse.TaskBroadcaster, *chi.Mux) {
	t.Helper()
	taskRepo := repo.NewTaskRepo(client)
	srRepo := repo.NewStageRunRepo(client)
	permRepo := repo.NewPermissionRepo(client)
	auditRepo := repo.NewAuditEventRepo(client)
	cfgRepo := repo.NewPipelineConfigRepo(client)

	broadcaster := sse.NewTaskBroadcaster(sse.NewBroadcaster())

	h := tasks.NewHandler(tasks.Deps{
		TaskRepo:     taskRepo,
		SRRepo:       srRepo,
		PermRepo:     permRepo,
		AuditRepo:    auditRepo,
		CfgRepo:      cfgRepo,
		Orchestrator: &noopOrchestrator{},
		Broadcaster:  broadcaster,
	})

	r := chi.NewRouter()
	r.Use(auth.RequireAuth(testJWTSecret))
	h.Mount(r)
	// Mount ingress routes without bearer middleware — unit tests drive them
	// directly and don't need McpAuthMiddleware.
	h.MountAgentIngress(r)
	return broadcaster, r
}

// noopOrchestrator satisfies tasks.OrchestratorIface without touching the DB.
type noopOrchestrator struct{}

func (n *noopOrchestrator) ProgressTask(_ context.Context, _ string, _ *pipeline.ProgressOpts) (*ent.StageRun, error) {
	return nil, nil
}
func (n *noopOrchestrator) ResumeFromUser(_ context.Context, _ string) (*ent.StageRun, error) {
	return nil, nil
}
func (n *noopOrchestrator) NotifyTaskTerminated(_ context.Context, _, _ string) {}
func (n *noopOrchestrator) InvalidateConfigCache()                               {}

func TestListTasks_Empty(t *testing.T) {
	_, r := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req = withAuth(t, req)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var result []any
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty list, got %d items", len(result))
	}
}

func TestCreateTask_Success(t *testing.T) {
	_, r := newTestHandler(t)

	body := map[string]any{
		"slug":  "my-task",
		"title": "My Task",
		"cwd":   "/tmp/test",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = withAuth(t, req)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["slug"] != "my-task" {
		t.Errorf("expected slug=my-task, got %v", result["slug"])
	}
}

func TestGetPipelineConfig_ReturnsRetryKeys(t *testing.T) {
	_, r := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/pipeline/config", nil)
	req = withAuth(t, req)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var result map[string]int
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := result["maxAutoRetries"]; !ok {
		t.Error("expected maxAutoRetries key in pipeline config response")
	}
	if _, ok := result["retryBackoffSeconds"]; !ok {
		t.Error("expected retryBackoffSeconds key in pipeline config response")
	}
	if result["maxAutoRetries"] != 3 {
		t.Errorf("expected default maxAutoRetries=3, got %d", result["maxAutoRetries"])
	}
	if result["retryBackoffSeconds"] != 60 {
		t.Errorf("expected default retryBackoffSeconds=60, got %d", result["retryBackoffSeconds"])
	}
}

func TestPutPipelineConfig_RetryKeys(t *testing.T) {
	_, r := newTestHandler(t)

	body := map[string]any{
		"maxAutoRetries":      5,
		"retryBackoffSeconds": 120,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/pipeline/config", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = withAuth(t, req)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var result map[string]int
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["maxAutoRetries"] != 5 {
		t.Errorf("expected maxAutoRetries=5, got %d", result["maxAutoRetries"])
	}
	if result["retryBackoffSeconds"] != 120 {
		t.Errorf("expected retryBackoffSeconds=120, got %d", result["retryBackoffSeconds"])
	}
}

func TestCreateTask_DuplicateSlug(t *testing.T) {
	_, r := newTestHandler(t)

	body := map[string]any{
		"slug":  "dup-slug",
		"title": "First",
		"cwd":   "/tmp/test",
	}
	b, _ := json.Marshal(body)

	// First creation — should succeed.
	req1 := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(b))
	req1.Header.Set("Content-Type", "application/json")
	req1 = withAuth(t, req1)
	rr1 := httptest.NewRecorder()
	r.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d: %s", rr1.Code, rr1.Body.String())
	}

	// Second creation with same slug — should return 409.
	b2, _ := json.Marshal(body)
	req2 := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(b2))
	req2.Header.Set("Content-Type", "application/json")
	req2 = withAuth(t, req2)
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusConflict {
		t.Fatalf("duplicate create: expected 409, got %d: %s", rr2.Code, rr2.Body.String())
	}
}
