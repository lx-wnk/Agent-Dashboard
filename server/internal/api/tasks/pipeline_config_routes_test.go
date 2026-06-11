package tasks_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

// newTestHandlerWithRealOrchestrator returns a handler backed by an in-memory DB
// and a real PipelineOrchestrator, so EffectiveStageModel resolves from the DB.
func newTestHandlerWithRealOrchestrator(t *testing.T) *chi.Mux {
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

	orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
		TaskRepo:       taskRepo,
		StageRunRepo:   srRepo,
		PermissionRepo: permRepo,
		AuditRepo:      auditRepo,
		ConfigRepo:     cfgRepo,
	})
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	h := tasks.NewHandler(tasks.Deps{
		TaskRepo:     taskRepo,
		SRRepo:       srRepo,
		PermRepo:     permRepo,
		AuditRepo:    auditRepo,
		CfgRepo:      cfgRepo,
		Orchestrator: orch,
		Broadcaster:  sse.NewTaskBroadcaster(sse.NewBroadcaster()),
	})
	r := chi.NewRouter()
	r.Use(auth.RequireAuth(testJWTSecret))
	h.Mount(r)
	return r
}

func TestGetPipelineConfig_DefaultStageModels(t *testing.T) {
	_, r := newTestHandler(t)

	req := withAuth(t, httptest.NewRequest(http.MethodGet, "/api/pipeline/config", nil))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		MaxParallelOrchestrators int               `json:"maxParallelOrchestrators"`
		StageTimeoutSeconds      int               `json:"stageTimeoutSeconds"`
		StageModels              map[string]string `json:"stageModels"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.MaxParallelOrchestrators != 3 {
		t.Errorf("expected maxParallelOrchestrators=3, got %d", resp.MaxParallelOrchestrators)
	}
	if resp.StageTimeoutSeconds != 1800 {
		t.Errorf("expected stageTimeoutSeconds=1800, got %d", resp.StageTimeoutSeconds)
	}

	// noopOrchestrator returns coded defaults.
	wantModels := map[string]string{
		"implementation": "claude-opus-4-6",
		"self_review":    "claude-sonnet-4-6",
		"finalization":   "claude-haiku-4-5",
	}
	for stage, want := range wantModels {
		got, ok := resp.StageModels[stage]
		if !ok {
			t.Errorf("stageModels missing key %q", stage)
			continue
		}
		if got != want {
			t.Errorf("stageModels[%q]: expected %q, got %q", stage, want, got)
		}
	}
	if len(resp.StageModels) != len(wantModels) {
		t.Errorf("stageModels has %d keys, expected %d", len(resp.StageModels), len(wantModels))
	}
}

func TestPutPipelineConfig_StageModelOverrideRoundTrip(t *testing.T) {
	// Real orchestrator so EffectiveStageModel resolves from the DB row we write.
	r := newTestHandlerWithRealOrchestrator(t)

	// PUT — override only self_review.
	body := map[string]any{
		"stageModels": map[string]string{
			"self_review": "claude-haiku-4-5",
		},
	}
	b, _ := json.Marshal(body)
	req := withAuth(t, httptest.NewRequest(http.MethodPut, "/api/pipeline/config", bytes.NewReader(b)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		StageModels map[string]string `json:"stageModels"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}
	if resp.StageModels["self_review"] != "claude-haiku-4-5" {
		t.Errorf("after PUT: self_review=%q, want claude-haiku-4-5", resp.StageModels["self_review"])
	}
	// Other stages should still return coded defaults.
	if resp.StageModels["implementation"] != "claude-opus-4-6" {
		t.Errorf("after PUT: implementation=%q, want claude-opus-4-6", resp.StageModels["implementation"])
	}
	if resp.StageModels["finalization"] != "claude-haiku-4-5" {
		t.Errorf("after PUT: finalization=%q, want claude-haiku-4-5", resp.StageModels["finalization"])
	}

	// GET should round-trip the persisted override.
	req2 := withAuth(t, httptest.NewRequest(http.MethodGet, "/api/pipeline/config", nil))
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}
	var resp2 struct {
		StageModels map[string]string `json:"stageModels"`
	}
	if err := json.Unmarshal(rr2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if resp2.StageModels["self_review"] != "claude-haiku-4-5" {
		t.Errorf("GET round-trip: self_review=%q, want claude-haiku-4-5", resp2.StageModels["self_review"])
	}
}

func TestPutPipelineConfig_EmptyModelIgnored(t *testing.T) {
	_, r := newTestHandler(t)

	// Empty string for self_review — should be ignored, not written.
	body := map[string]any{
		"stageModels": map[string]string{
			"self_review": "",
		},
	}
	b, _ := json.Marshal(body)
	req := withAuth(t, httptest.NewRequest(http.MethodPut, "/api/pipeline/config", bytes.NewReader(b)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		StageModels map[string]string `json:"stageModels"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// noopOrchestrator still returns the coded default because the empty value was not persisted.
	if resp.StageModels["self_review"] != "claude-sonnet-4-6" {
		t.Errorf("empty model should be ignored; got %q", resp.StageModels["self_review"])
	}
}

func TestPutPipelineConfig_UnknownStageIgnored(t *testing.T) {
	_, r := newTestHandler(t)

	body := map[string]any{
		"stageModels": map[string]string{
			"nonexistent_stage": "some-model",
		},
	}
	b, _ := json.Marshal(body)
	req := withAuth(t, httptest.NewRequest(http.MethodPut, "/api/pipeline/config", bytes.NewReader(b)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	// Just verify the response still has exactly the 3 valid stage keys.
	var resp struct {
		StageModels map[string]string `json:"stageModels"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, bad := resp.StageModels["nonexistent_stage"]; bad {
		t.Errorf("unknown stage key leaked into GET response")
	}
	if len(resp.StageModels) != 3 {
		t.Errorf("expected 3 stageModels keys, got %d", len(resp.StageModels))
	}
}
