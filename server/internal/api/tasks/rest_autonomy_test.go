package tasks_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateTask_WithAutonomy_Persists(t *testing.T) {
	_, r := newTestHandler(t)

	autonomy := "full"
	body := map[string]any{
		"slug":     "aut-full",
		"title":    "Full Autonomy Task",
		"cwd":      "/tmp/aut-full",
		"autonomy": autonomy,
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
	if result["autonomy"] != autonomy {
		t.Errorf("expected autonomy=%s, got %v", autonomy, result["autonomy"])
	}
}

func TestCreateTask_InvalidAutonomy_Returns400(t *testing.T) {
	_, r := newTestHandler(t)

	body := map[string]any{
		"slug":     "aut-bogus",
		"title":    "Bogus Autonomy Task",
		"cwd":      "/tmp/aut-bogus",
		"autonomy": "bogus",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = withAuth(t, req)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUpdateTask_Autonomy_Persists(t *testing.T) {
	_, r := newTestHandler(t)

	// Create a task first.
	createBody := map[string]any{
		"slug":  "aut-update",
		"title": "Update Autonomy Task",
		"cwd":   "/tmp/aut-update",
	}
	b, _ := json.Marshal(createBody)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = withAuth(t, req)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &created)
	id := created["id"].(string)

	// Patch autonomy.
	autonomy := "manual"
	patchBody := map[string]any{"autonomy": autonomy}
	b, _ = json.Marshal(patchBody)
	req = httptest.NewRequest(http.MethodPatch, "/api/tasks/"+id, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = withAuth(t, req)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var updated map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	if updated["autonomy"] != autonomy {
		t.Errorf("expected autonomy=%s, got %v", autonomy, updated["autonomy"])
	}
}
