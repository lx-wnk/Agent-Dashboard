package hooks

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
)

const testSecret = "s3cret"

func newBridgeHandler(t *testing.T) *Handler {
	t.Helper()
	h := newTestHandler(testSecret)
	// Most tests here exercise what happens once a session is intercepted, so
	// they arm it. The gate itself is covered by the tests that do not.
	h.permissions.Arm("s1", true)
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
	pending, _ := pendingOf(t, h, "s1")
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

	if pending, _ := pendingOf(t, h, "s2"); len(pending) != 0 {
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
	if _, atTerminal := pendingOf(t, h, "s1"); !atTerminal {
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

	if _, atTerminal := pendingOf(t, h, "s1"); atTerminal {
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

	if _, atTerminal := pendingOf(t, h, "s1"); atTerminal {
		t.Fatal("a held request left the stale terminal notice in place")
	}
}

func TestTerminalNoticeAgesOut(t *testing.T) {
	h := newBridgeHandler(t)
	now := time.Now()
	h.permissions.nowFn = func() time.Time { return now }

	h.permissions.noteTerminalPrompt("s1")
	if _, atTerminal := pendingOf(t, h, "s1"); !atTerminal {
		t.Fatal("notice was not recorded")
	}

	now = now.Add(permissionNoticeTTL + time.Second)
	if _, atTerminal := pendingOf(t, h, "s1"); atTerminal {
		t.Fatal("notice outlived its TTL — an answered prompt would look open forever")
	}
}

func waitForPendingID(t *testing.T, h *Handler, sessionID string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pending, _ := pendingOf(t, h, sessionID); len(pending) > 0 {
			return pending[0].ID
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no pending request appeared for session %q", sessionID)
	return ""
}

// pendingOf adapts StateForSession to the (held, atTerminal) shape these tests
// were written against.
func pendingOf(t *testing.T, h *Handler, sessionID string) ([]sdk.PendingPermission, bool) {
	t.Helper()
	held, atTerminal, _ := h.permissions.StateForSession(sessionID)
	return held, atTerminal
}

// The load-bearing property of the gate: PreToolUse fires before Claude Code
// decides whether to prompt, so an unarmed session must be answered instantly
// with no decision. Measured before this gate existed: one allow-listed Read
// took 38 seconds.
func TestUnarmedSessionIsNotHeld(t *testing.T) {
	h := newTestHandler(testSecret) // deliberately NOT armed
	h.permissions.holdFor = 5 * time.Second

	start := time.Now()
	rec := post(t, h.PermissionRequest, "/api/hooks/permission", preToolBody("s-cold", "Bash", "ls"), true)
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("an unarmed session was held for %s — the gate did not fire", elapsed)
	}
	if body := rec.Body.String(); body != "{}\n" {
		t.Fatalf("body = %q, want an empty object so the hook prints nothing", body)
	}
	if held, _, _ := h.permissions.StateForSession("s-cold"); len(held) != 0 {
		t.Fatalf("an unarmed session produced %d held requests", len(held))
	}
}

func TestArmingIsPerSession(t *testing.T) {
	h := newBridgeHandler(t) // arms s1 only
	h.permissions.holdFor = 5 * time.Second

	start := time.Now()
	post(t, h.PermissionRequest, "/api/hooks/permission", preToolBody("s2", "Bash", "ls"), true)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("session s2 was held for %s although only s1 is armed", elapsed)
	}
}

func TestDisarmStopsHolding(t *testing.T) {
	h := newBridgeHandler(t)
	h.permissions.holdFor = 5 * time.Second
	h.permissions.Arm("s1", false)

	start := time.Now()
	post(t, h.PermissionRequest, "/api/hooks/permission", preToolBody("s1", "Bash", "ls"), true)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("a disarmed session was held for %s", elapsed)
	}
}

func TestArmingExpires(t *testing.T) {
	h := newBridgeHandler(t)
	h.permissions.holdFor = 5 * time.Second
	now := time.Now()
	h.permissions.nowFn = func() time.Time { return now }
	h.permissions.Arm("s1", true)

	now = now.Add(armedTTL + time.Minute)
	start := time.Now()
	post(t, h.PermissionRequest, "/api/hooks/permission", preToolBody("s1", "Bash", "ls"), true)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("an expired arm still held for %s — a forgotten arm would stall the session forever", elapsed)
	}
}

// Past the cap the answer is "no decision", the same fail-safe a lapse gives,
// so a runaway batch degrades to terminal prompts rather than goroutines.
func TestConcurrentHoldsAreCapped(t *testing.T) {
	h := newBridgeHandler(t)
	h.permissions.holdFor = 3 * time.Second

	for range maxHoldsPerSession {
		go func() {
			_ = post(t, h.PermissionRequest, "/api/hooks/permission", preToolBody("s1", "Bash", "sleep"), true)
		}()
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if held, _, _ := h.permissions.StateForSession("s1"); len(held) == maxHoldsPerSession {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	start := time.Now()
	post(t, h.PermissionRequest, "/api/hooks/permission", preToolBody("s1", "Bash", "one too many"), true)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("the request past the cap was held for %s", elapsed)
	}
	if held, _, _ := h.permissions.StateForSession("s1"); len(held) > maxHoldsPerSession {
		t.Fatalf("held %d requests, cap is %d", len(held), maxHoldsPerSession)
	}
}

// RequestedAt has second precision, so a batch of parallel calls would sort
// arbitrarily and the card could answer a request other than the one it showed.
func TestHeldOrderIsStableWithinOneSecond(t *testing.T) {
	h := newBridgeHandler(t)
	h.permissions.holdFor = 3 * time.Second
	frozen := time.Now()
	h.permissions.nowFn = func() time.Time { return frozen }

	for _, cmd := range []string{"first", "second", "third"} {
		go func() {
			_ = post(t, h.PermissionRequest, "/api/hooks/permission", preToolBody("s1", "Bash", cmd), true)
		}()
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if held, _, _ := h.permissions.StateForSession("s1"); len(held) == 3 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	first, _, _ := h.permissions.StateForSession("s1")
	if len(first) != 3 {
		t.Fatalf("expected 3 held requests, got %d", len(first))
	}
	for range 50 {
		again, _, _ := h.permissions.StateForSession("s1")
		for i := range first {
			if again[i].ID != first[i].ID {
				t.Fatalf("order changed between reads at index %d: %q then %q", i, first[i].ID, again[i].ID)
			}
		}
	}
}

// The read path must not mutate: expiry belongs to the sweep, so that a session
// nobody polls still has its notice cleaned up.
func TestStateForSessionDoesNotExpire(t *testing.T) {
	h := newBridgeHandler(t)
	now := time.Now()
	h.permissions.nowFn = func() time.Time { return now }
	h.permissions.noteTerminalPrompt("s-gone")

	now = now.Add(permissionNoticeTTL + time.Minute)
	if _, atTerminal, _ := h.permissions.StateForSession("s-gone"); atTerminal {
		t.Fatal("an aged-out notice was still reported")
	}
	h.permissions.mu.Lock()
	stillThere := len(h.permissions.notices)
	h.permissions.mu.Unlock()
	if stillThere == 0 {
		t.Fatal("the read path deleted the notice — expiry must come from SweepExpired")
	}

	h.permissions.SweepExpired()
	h.permissions.mu.Lock()
	after := len(h.permissions.notices)
	h.permissions.mu.Unlock()
	if after != 0 {
		t.Fatalf("SweepExpired left %d notices behind", after)
	}
}
