package hooks

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/permissions"
)

// newTestHandler is a convenience constructor for tests.
// onEvent is always a no-op — tests verify HTTP behaviour, not side effects.
func newTestHandler(secret string) *Handler {
	return New(secret, func() {})
}

// -------------------------------------------------------------------
// Event endpoint
// -------------------------------------------------------------------

func TestEvent_WithCorrectSecret_Returns204(t *testing.T) {
	h := newTestHandler("mysecret")
	req := httptest.NewRequest(http.MethodPost, "/api/hooks/event", nil)
	req.Header.Set("Authorization", "Bearer mysecret")
	w := httptest.NewRecorder()

	h.Event(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Event with correct secret: got status %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestEvent_WithWrongSecret_Returns401(t *testing.T) {
	h := newTestHandler("mysecret")
	req := httptest.NewRequest(http.MethodPost, "/api/hooks/event", nil)
	req.Header.Set("Authorization", "Bearer wrongsecret")
	w := httptest.NewRecorder()

	h.Event(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Event with wrong secret: got status %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestEvent_WithNoSecretConfigured_IsOpenWithoutAuth(t *testing.T) {
	// When DASHBOARD_HOOKS_SECRET is empty the event endpoint accepts any request
	// (server is loopback-only so this is intentional for local use).
	h := newTestHandler("")
	req := httptest.NewRequest(http.MethodPost, "/api/hooks/event", nil)
	// No Authorization header at all.
	w := httptest.NewRecorder()

	h.Event(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Event with no secret configured (no auth header): got status %d, want %d", w.Code, http.StatusNoContent)
	}
}

// -------------------------------------------------------------------
// PreTool endpoint
// -------------------------------------------------------------------

func TestPreTool_WithoutSecret_Returns401(t *testing.T) {
	// PreTool requires a secret even when the Event endpoint is open.
	h := newTestHandler("")
	body := `{"toolName":"Bash","cwd":"/tmp"}`
	req := httptest.NewRequest(http.MethodPost, "/api/hooks/pre-tool", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.PreTool(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("PreTool without secret configured: got status %d, want %d (secret required)", w.Code, http.StatusUnauthorized)
	}
}

func TestPreTool_NonWriteTool_ReturnsProceedImmediately(t *testing.T) {
	// Tools that are not Edit/Write/MultiEdit bypass the gate and return immediately.
	const secret = "s3cr3t"
	h := newTestHandler(secret)
	body := `{"sessionId":"sess1","toolName":"Bash","cwd":"/tmp"}`
	req := httptest.NewRequest(http.MethodPost, "/api/hooks/pre-tool", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)
	w := httptest.NewRecorder()

	h.PreTool(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("PreTool Bash tool: got status %d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("PreTool Bash tool: decode response: %v", err)
	}
	proceed, ok := resp["proceed"]
	if !ok {
		t.Fatalf("PreTool Bash tool: response missing 'proceed' field, got %v", resp)
	}
	if proceed != true {
		t.Errorf("PreTool Bash tool: 'proceed' = %v, want true", proceed)
	}
}

func TestPreTool_WriteTool_TimeoutReturnsProceeds(t *testing.T) {
	// Write-type tools register a pending gate and block until timeout or decision.
	// This test overrides the timeout by sending a response via the Respond endpoint
	// immediately after PreTool registers the pending entry.
	const secret = "s3cr3t"
	h := newTestHandler(secret)

	// Run PreTool in a goroutine since it blocks waiting for a decision.
	preToolDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		body := `{"sessionId":"sess1","toolName":"Edit","filePath":"/tmp/foo.go","oldContent":"a","newContent":"b"}`
		req := httptest.NewRequest(http.MethodPost, "/api/hooks/pre-tool", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		h.PreTool(w, req)
		preToolDone <- w
	}()

	// Poll until the pending entry appears, then send an accept decision.
	var pendingID string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		for id := range h.pending {
			pendingID = id
		}
		h.mu.Unlock()
		if pendingID != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if pendingID == "" {
		t.Fatal("PreTool Edit: pending entry never appeared")
	}

	// Accept the pending edit via Respond.
	respondBody := map[string]string{"id": pendingID, "decision": "accept"}
	buf, _ := json.Marshal(respondBody)
	respondReq := httptest.NewRequest(http.MethodPost, "/api/hooks/respond", bytes.NewReader(buf))
	respondReq.Header.Set("Content-Type", "application/json")
	respondReq.Header.Set("Authorization", "Bearer "+secret)
	respondW := httptest.NewRecorder()
	h.Respond(respondW, respondReq)

	if respondW.Code != http.StatusOK {
		t.Fatalf("Respond: got status %d, want 200", respondW.Code)
	}

	// Wait for PreTool to finish.
	preToolW := <-preToolDone
	if preToolW.Code != http.StatusOK {
		t.Fatalf("PreTool Edit after accept: got status %d, want 200", preToolW.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(preToolW.Body).Decode(&resp); err != nil {
		t.Fatalf("PreTool Edit: decode response: %v", err)
	}
	if resp["proceed"] != true {
		t.Errorf("PreTool Edit accepted: 'proceed' = %v, want true", resp["proceed"])
	}
}

// -------------------------------------------------------------------
// Pending endpoint
// -------------------------------------------------------------------

func TestPending_WithoutSecret_Returns401(t *testing.T) {
	h := newTestHandler("")
	req := httptest.NewRequest(http.MethodGet, "/api/hooks/pending", nil)
	w := httptest.NewRecorder()

	h.Pending(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Pending without secret: got status %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestPending_WithSecret_ReturnsValidJSON(t *testing.T) {
	const secret = "s3cr3t"
	h := newTestHandler(secret)
	req := httptest.NewRequest(http.MethodGet, "/api/hooks/pending", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	w := httptest.NewRecorder()

	h.Pending(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Pending: got status %d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Pending: response is not valid JSON: %v", err)
	}
	if _, ok := resp["edits"]; !ok {
		t.Errorf("Pending: response missing 'edits' key, got %v", resp)
	}
}

// TestWriteToolNames_MatchesHookGate asserts that every name in permissions.WriteToolNames
// is treated as a write tool by isWriteTool, and that isWriteTool returns false for
// a known non-write tool. This guards against the two lists drifting apart.
func TestWriteToolNames_MatchesHookGate(t *testing.T) {
	for _, name := range permissions.WriteToolNames {
		if !permissions.IsWriteTool(name) {
			t.Errorf("IsWriteTool(%q) = false, want true — permissions.WriteToolNames and the gate are out of sync", name)
		}
	}
	// A tool that must never be gated.
	if permissions.IsWriteTool("Bash") {
		t.Error("IsWriteTool(\"Bash\") = true, want false")
	}
}

func TestPending_WithSecret_ReturnsEmptyEditsWhenNoPending(t *testing.T) {
	const secret = "s3cr3t"
	h := newTestHandler(secret)
	req := httptest.NewRequest(http.MethodGet, "/api/hooks/pending", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	w := httptest.NewRecorder()

	h.Pending(w, req)

	var resp struct {
		Edits []PendingEdit `json:"edits"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Pending: decode: %v", err)
	}
	if len(resp.Edits) != 0 {
		t.Errorf("Pending with no active gates: got %d edits, want 0", len(resp.Edits))
	}
}
