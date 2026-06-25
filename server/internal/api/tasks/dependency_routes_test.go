package tasks_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// postDependency issues an authed POST to the add-dependency route and returns the recorder.
func postDependency(t *testing.T, r http.Handler, taskID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+taskID+"/dependencies", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = withAuth(t, req)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestAddDependency_RejectsInvalidRequiredStage(t *testing.T) {
	_, r := newTestHandler(t)
	rr := postDependency(t, r, "task-x", map[string]any{
		"dependsOnId":   "task-y",
		"requiredStage": "implementation",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid requiredStage, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAddDependency_RejectsInvalidOnCancelAction(t *testing.T) {
	_, r := newTestHandler(t)
	rr := postDependency(t, r, "task-x", map[string]any{
		"dependsOnId":    "task-y",
		"onCancelAction": "nothing",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid onCancelAction, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAddDependency_RequiresDependsOnId(t *testing.T) {
	_, r := newTestHandler(t)
	rr := postDependency(t, r, "task-x", map[string]any{})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing dependsOnId, got %d: %s", rr.Code, rr.Body.String())
	}
}
