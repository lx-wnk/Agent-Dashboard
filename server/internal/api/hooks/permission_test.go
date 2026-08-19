package hooks

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testSecret = "s3cret"

func newBridgeHandler(t *testing.T) *Handler {
	t.Helper()
	h := newTestHandler(testSecret)
	// Keep the hold short: the tests that exercise the lapse would otherwise sit
	// out the production window, and the value under test is the direction of
	// the fallback, not its length.
	h.permissions.holdFor = 150 * time.Millisecond
	return h
}

func post(t *testing.T, fn http.HandlerFunc, path string, body any, withSecret bool) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	if withSecret {
		req.Header.Set("Authorization", "Bearer "+testSecret)
	}
	rec := httptest.NewRecorder()
	fn(rec, req)
	return rec
}

func preToolBody(sessionID, tool, command string) map[string]any {
	return map[string]any{
		"session_id":      sessionID,
		"tool_name":       tool,
		"tool_use_id":     "toolu_1",
		"permission_mode": "default",
		"tool_input":      map[string]string{"command": command},
	}
}

// The whole safety story rests on this: when nobody answers, the response must
// carry NO decision, so Claude Code falls through to its own terminal prompt.
// A response that said "allow" here would silently approve every tool call the
// moment the dashboard stopped answering.
func TestPermissionRequestLapsesWithoutDeciding(t *testing.T) {
	h := newBridgeHandler(t)

	rec := post(t, h.PermissionRequest, "/api/hooks/permission", preToolBody("s1", "Bash", "rm -rf /tmp/x"), true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body is not JSON: %q", rec.Body.String())
	}
	if _, ok := out["hookSpecificOutput"]; ok {
		t.Fatalf("a lapsed hold returned a decision: %q", rec.Body.String())
	}
	if len(out) != 0 {
		t.Fatalf("body = %q, want an empty object so the hook prints nothing", rec.Body.String())
	}
}

func TestPermissionRequestReturnsTheDashboardDecision(t *testing.T) {
	for _, decision := range []string{"allow", "deny"} {
		t.Run(decision, func(t *testing.T) {
			h := newBridgeHandler(t)
			h.permissions.holdFor = 3 * time.Second

			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				done <- post(t, h.PermissionRequest, "/api/hooks/permission", preToolBody("s1", "Bash", "npm publish"), true)
			}()

			id := waitForPendingID(t, h, "s1")
			rr := post(t, h.PermissionRespond, "/api/hooks/permission/respond",
				map[string]string{"id": id, "decision": decision}, false)
			if rr.Code != http.StatusOK {
				t.Fatalf("respond status = %d: %s", rr.Code, rr.Body.String())
			}

			rec := <-done
			var out struct {
				HookSpecificOutput struct {
					HookEventName      string `json:"hookEventName"`
					PermissionDecision string `json:"permissionDecision"`
				} `json:"hookSpecificOutput"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("body is not JSON: %q", rec.Body.String())
			}
			if out.HookSpecificOutput.PermissionDecision != decision {
				t.Fatalf("permissionDecision = %q, want %q", out.HookSpecificOutput.PermissionDecision, decision)
			}
			if out.HookSpecificOutput.HookEventName != "PreToolUse" {
				t.Fatalf("hookEventName = %q, want PreToolUse", out.HookSpecificOutput.HookEventName)
			}
		})
	}
}

// The bearer secret is what stops any local page from answering permission
// prompts on the user's behalf.
func TestPermissionRequestRequiresTheSecret(t *testing.T) {
	h := newBridgeHandler(t)
	rec := post(t, h.PermissionRequest, "/api/hooks/permission", preToolBody("s1", "Bash", "ls"), false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestPermissionRespondRejectsUnknownAndBadDecisions(t *testing.T) {
	h := newBridgeHandler(t)

	rec := post(t, h.PermissionRespond, "/api/hooks/permission/respond",
		map[string]string{"id": "does-not-exist", "decision": "allow"}, false)
	if rec.Code != http.StatusConflict {
		t.Fatalf("unknown id status = %d, want 409", rec.Code)
	}

	rec = post(t, h.PermissionRespond, "/api/hooks/permission/respond",
		map[string]string{"id": "x", "decision": "maybe"}, false)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad decision status = %d, want 400", rec.Code)
	}
}

// The pattern is taken from the producer, not reconstructed from the
// transcript, so it is the argument the session is actually asking about.
func TestPendingCarriesTheToolArgument(t *testing.T) {
	h := newBridgeHandler(t)
	h.permissions.holdFor = 3 * time.Second
	go func() {
		_ = post(t, h.PermissionRequest, "/api/hooks/permission", preToolBody("s1", "Bash", "npm publish --access public"), true)
	}()

	id := waitForPendingID(t, h, "s1")
	pending, _ := h.permissions.PendingForSession("s1")
	if len(pending) != 1 || pending[0].ID != id {
		t.Fatalf("pending = %+v, want exactly the held request", pending)
	}
	if pending[0].Pattern == nil || *pending[0].Pattern != "npm publish --access public" {
		t.Fatalf("pattern = %v, want the command from tool_input", pending[0].Pattern)
	}
	if pending[0].Tool != "Bash" {
		t.Fatalf("tool = %q, want Bash", pending[0].Tool)
	}
}

func TestPendingIsScopedToItsSession(t *testing.T) {
	h := newBridgeHandler(t)
	h.permissions.holdFor = 3 * time.Second
	go func() {
		_ = post(t, h.PermissionRequest, "/api/hooks/permission", preToolBody("s1", "Bash", "ls"), true)
	}()
	waitForPendingID(t, h, "s1")

	if pending, _ := h.permissions.PendingForSession("s2"); len(pending) != 0 {
		t.Fatalf("session s2 saw %d requests belonging to s1", len(pending))
	}
}

// A Notification means the hold already lapsed and the terminal is asking, so
// it is reported but not answerable here.
func TestNotificationMarksTheTerminalPrompt(t *testing.T) {
	h := newBridgeHandler(t)

	rec := post(t, h.PermissionNotify, "/api/hooks/notification", map[string]string{
		"session_id":        "s1",
		"notification_type": "permission_prompt",
		"message":           "Claude needs your permission",
	}, true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if _, atTerminal := h.permissions.PendingForSession("s1"); !atTerminal {
		t.Fatal("the terminal prompt was not recorded")
	}
}

// Every other notification_type must be ignored: matching on the message prose
// instead would misread an unrelated notice as a permission prompt.
func TestNotificationIgnoresOtherTypes(t *testing.T) {
	h := newBridgeHandler(t)

	post(t, h.PermissionNotify, "/api/hooks/notification", map[string]string{
		"session_id":        "s1",
		"notification_type": "idle_reminder",
		"message":           "Claude needs your permission to keep going",
	}, true)

	if _, atTerminal := h.permissions.PendingForSession("s1"); atTerminal {
		t.Fatal("an unrelated notification was read as a permission prompt")
	}
}

// A fresh hold means the decision is ours again, so a stale terminal notice for
// the same session must not keep claiming the user has to go look.
func TestHoldClearsAnEarlierTerminalNotice(t *testing.T) {
	h := newBridgeHandler(t)
	h.permissions.holdFor = 3 * time.Second

	h.permissions.noteTerminalPrompt("s1")
	go func() {
		_ = post(t, h.PermissionRequest, "/api/hooks/permission", preToolBody("s1", "Bash", "ls"), true)
	}()
	waitForPendingID(t, h, "s1")

	if _, atTerminal := h.permissions.PendingForSession("s1"); atTerminal {
		t.Fatal("a held request left the stale terminal notice in place")
	}
}

func TestTerminalNoticeAgesOut(t *testing.T) {
	h := newBridgeHandler(t)
	now := time.Now()
	h.permissions.nowFn = func() time.Time { return now }

	h.permissions.noteTerminalPrompt("s1")
	if _, atTerminal := h.permissions.PendingForSession("s1"); !atTerminal {
		t.Fatal("notice was not recorded")
	}

	now = now.Add(permissionNoticeTTL + time.Second)
	if _, atTerminal := h.permissions.PendingForSession("s1"); atTerminal {
		t.Fatal("notice outlived its TTL — an answered prompt would look open forever")
	}
}

func waitForPendingID(t *testing.T, h *Handler, sessionID string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pending, _ := h.permissions.PendingForSession(sessionID); len(pending) > 0 {
			return pending[0].ID
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no pending request appeared for session %q", sessionID)
	return ""
}
