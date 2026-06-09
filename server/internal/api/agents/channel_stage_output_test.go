package agents_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/agents"
	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
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

// writeDiscoveryFile creates the per-PID discovery file under the given home dir.
func writeDiscoveryFile(t *testing.T, home string, pid int, token string) {
	t.Helper()
	dir := filepath.Join(home, channelconfig.DiscoveryDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir discovery dir: %v", err)
	}
	data, _ := json.Marshal(map[string]string{"token": token})
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".json"), data, 0o600); err != nil {
		t.Fatalf("write discovery file: %v", err)
	}
}

func TestChannelStageOutput_ValidImplementation_Persists(t *testing.T) {
	pid := 4242
	tok := "tok-abc"

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeDiscoveryFile(t, home, pid, tok)

	pidVal := pid
	fake := &fakeStageRunRepo{
		run: &ent.StageRun{ID: "run-1", Stage: "implementation", Pid: &pidVal},
	}

	h := agents.NewChannelStageOutputHandler(fake)

	body, _ := json.Marshal(map[string]any{
		"stageRunId": "run-1",
		"output": map[string]any{
			"summary":   "did it",
			"commits":   []any{"abc"},
			"openItems": []any{},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/channel-stage-output", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
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
	pid := 4242
	tok := "tok-abc"

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeDiscoveryFile(t, home, pid, tok)

	pidVal := pid
	fake := &fakeStageRunRepo{
		run: &ent.StageRun{ID: "run-1", Stage: "implementation", Pid: &pidVal},
	}

	h := agents.NewChannelStageOutputHandler(fake)

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
	pid := 9999
	tok := "tok-xyz"

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeDiscoveryFile(t, home, pid, tok)

	pidVal := pid
	fake := &fakeStageRunRepo{
		run: &ent.StageRun{ID: "run-2", Stage: "self_review", Pid: &pidVal},
	}

	h := agents.NewChannelStageOutputHandler(fake)

	// empty output is missing required self_review fields
	body, _ := json.Marshal(map[string]any{
		"stageRunId": "run-2",
		"output":     map[string]any{},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/channel-stage-output", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
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
	home := t.TempDir()
	t.Setenv("HOME", home)

	fake := &fakeStageRunRepo{
		getErr: context.DeadlineExceeded, // any non-nil error
	}

	h := agents.NewChannelStageOutputHandler(fake)

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
	home := t.TempDir()
	t.Setenv("HOME", home)

	fake := &fakeStageRunRepo{}

	h := agents.NewChannelStageOutputHandler(fake)

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
