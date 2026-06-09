package agents_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/agents"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// fakeStageRunRepo implements repo.StageRunRepo with only GetByID and Update wired.
type fakeStageRunRepo struct {
	repo.StageRunRepo
	run           *ent.StageRun
	getErr        error
	capturedInput *repo.UpdateStageRunInput
}

func (f *fakeStageRunRepo) GetByID(_ context.Context, _ string) (*ent.StageRun, error) {
	return f.run, f.getErr
}

func (f *fakeStageRunRepo) Update(_ context.Context, _ string, in repo.UpdateStageRunInput) (*ent.StageRun, error) {
	f.capturedInput = &in
	return f.run, nil
}

// fakeApiKeyRepo implements repo.ApiKeyRepo; GetByHash returns a valid key or error.
type fakeApiKeyRepo struct {
	repo.ApiKeyRepo
	valid bool
}

func (f *fakeApiKeyRepo) GetByHash(_ context.Context, _ string) (*ent.ApiKey, error) {
	if f.valid {
		return &ent.ApiKey{}, nil
	}
	return nil, errors.New("invalid token")
}

func TestChannelStageOutput_ValidImplementation_Persists(t *testing.T) {
	fake := &fakeStageRunRepo{
		run: &ent.StageRun{ID: "run-1", Stage: "implementation"},
	}
	fakeKeys := &fakeApiKeyRepo{valid: true}

	h := agents.NewChannelStageOutputHandler(fake, fakeKeys)

	body, _ := json.Marshal(map[string]any{
		"stageRunId": "run-1",
		"output": map[string]any{
			"summary":   "did it",
			"commits":   []any{"abc"},
			"openItems": []any{},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/channel-stage-output", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Post(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if fake.capturedInput == nil {
		t.Fatal("Update was not called")
	}
	if fake.capturedInput.Output["summary"] != "did it" {
		t.Errorf("unexpected output: %v", fake.capturedInput.Output)
	}
}

func TestChannelStageOutput_BadToken_401(t *testing.T) {
	fake := &fakeStageRunRepo{
		run: &ent.StageRun{ID: "run-1", Stage: "implementation"},
	}
	fakeKeys := &fakeApiKeyRepo{valid: false}

	h := agents.NewChannelStageOutputHandler(fake, fakeKeys)

	body, _ := json.Marshal(map[string]any{
		"stageRunId": "run-1",
		"output":     map[string]any{"summary": "did it"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/channel-stage-output", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer wrong")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Post(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if fake.capturedInput != nil {
		t.Error("Update should not have been called")
	}
}

func TestChannelStageOutput_SchemaInvalid_422(t *testing.T) {
	fake := &fakeStageRunRepo{
		run: &ent.StageRun{ID: "run-2", Stage: "self_review"},
	}
	fakeKeys := &fakeApiKeyRepo{valid: true}

	h := agents.NewChannelStageOutputHandler(fake, fakeKeys)

	// empty output is missing required self_review fields
	body, _ := json.Marshal(map[string]any{
		"stageRunId": "run-2",
		"output":     map[string]any{},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/channel-stage-output", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Post(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	if fake.capturedInput != nil {
		t.Error("Update should not have been called")
	}
}

func TestChannelStageOutput_UnknownStageRun_404(t *testing.T) {
	fake := &fakeStageRunRepo{
		getErr: context.DeadlineExceeded, // any non-nil error
	}
	// apiKeyRepo not reached — 404 fires before auth
	fakeKeys := &fakeApiKeyRepo{valid: true}

	h := agents.NewChannelStageOutputHandler(fake, fakeKeys)

	body, _ := json.Marshal(map[string]any{
		"stageRunId": "no-such-run",
		"output":     map[string]any{"x": 1},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/channel-stage-output", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer sometoken")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Post(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChannelStageOutput_WrongTypes_400(t *testing.T) {
	fake := &fakeStageRunRepo{}
	fakeKeys := &fakeApiKeyRepo{valid: true}

	h := agents.NewChannelStageOutputHandler(fake, fakeKeys)

	// output is an array, not an object — wrong type
	body, _ := json.Marshal(map[string]any{
		"stageRunId": "run-1",
		"output":     []any{"not", "an", "object"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/channel-stage-output", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Post(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if fake.capturedInput != nil {
		t.Error("Update should not have been called")
	}
}
