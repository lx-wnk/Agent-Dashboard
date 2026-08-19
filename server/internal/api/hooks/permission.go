package hooks

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/sdk"
)

const (
	// permissionHoldTimeout is how long a PreToolUse hook call is held while the
	// dashboard waits for a human. It must stay comfortably below the `timeout`
	// configured for the hook in settings: when the hook times out, Claude Code
	// falls back to its own terminal prompt (measured, not assumed), so lapsing
	// first on our side keeps that fallback clean instead of racing it.
	permissionHoldTimeout = 25 * time.Second

	// permissionNoticeTTL bounds how long a "the terminal is asking" notice
	// survives without a refresh. The Notification hook fires once when the
	// prompt opens and never again when it is answered, so the notice has to age
	// out rather than wait to be cleared.
	permissionNoticeTTL = 2 * time.Minute

	maxPermissionBodyBytes = 64 * 1024
	// maxPatternBytes bounds the tool argument taken from the hook payload. The
	// value is agent-authored and is displayed next to an approve control.
	maxPatternBytes = 4096
)

// permissionRequest is one PreToolUse call held open while a human decides.
type permissionRequest struct {
	perm      sdk.PendingPermission
	sessionID string
	toolUseID string
	ch        chan string // receives "allow" or "deny"
}

// permissionNotice records that a session's terminal is showing its own
// permission prompt — which happens when no dashboard decision arrived in time,
// or when the bridge hook is not installed at all.
type permissionNotice struct {
	at time.Time
}

// PermissionBridge holds PreToolUse hook calls open so a permission prompt can
// be answered in the dashboard instead of in the session's terminal.
//
// Two signals arrive, and they are mutually exclusive by construction:
//
//   - A held PreToolUse call means the decision is still ours to make. Claude
//     Code has not drawn its prompt yet; it is blocked on our answer.
//   - A Notification with notification_type "permission_prompt" means the hold
//     already lapsed (or never happened) and the terminal is now asking. That is
//     no longer answerable from here — it is evidence that someone must go look.
type PermissionBridge struct {
	mu       sync.Mutex
	pending  map[string]*permissionRequest // request id -> held call
	notices  map[string]permissionNotice   // session id -> terminal is asking
	nowFn    func() time.Time
	holdFor  time.Duration
	noticeFn func() // called when the pending set changes, to nudge a rescan
}

// NewPermissionBridge builds a bridge. onChange may be nil; when set it is
// called whenever the pending set changes so the caller can push a fresh
// snapshot to connected clients instead of waiting for the next scan tick.
func NewPermissionBridge(onChange func()) *PermissionBridge {
	return &PermissionBridge{
		pending:  map[string]*permissionRequest{},
		notices:  map[string]permissionNotice{},
		nowFn:    time.Now,
		holdFor:  permissionHoldTimeout,
		noticeFn: onChange,
	}
}

// SetOnChange installs the callback fired whenever the pending set changes.
// The bridge is built in the DI container, before the debounced rescan the
// router owns exists, so the callback arrives afterwards rather than at
// construction.
func (b *PermissionBridge) SetOnChange(fn func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.noticeFn = fn
}

func (b *PermissionBridge) changed() {
	b.mu.Lock()
	fn := b.noticeFn
	b.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// preToolPayload is the subset of Claude Code's PreToolUse payload the bridge uses.
type preToolPayload struct {
	SessionID      string          `json:"session_id"`
	ToolName       string          `json:"tool_name"`
	ToolUseID      string          `json:"tool_use_id"`
	ToolInput      json.RawMessage `json:"tool_input"`
	PermissionMode string          `json:"permission_mode"`
	CWD            string          `json:"cwd"`
}

// notificationPayload is the subset of Claude Code's Notification payload the
// bridge uses. NotificationType is a typed field rather than prose, which is
// what makes this usable as a signal at all.
type notificationPayload struct {
	SessionID        string `json:"session_id"`
	NotificationType string `json:"notification_type"`
	Message          string `json:"message"`
}

// permissionPromptNotification is the notification_type Claude Code sends when
// it draws its own permission prompt.
const permissionPromptNotification = "permission_prompt"

// Request handles POST /api/hooks/permission — the PreToolUse hook.
//
// It answers with the JSON the hook must print verbatim. An empty object means
// "no decision": the hook prints nothing and Claude Code proceeds with its
// normal flow, which is its terminal prompt. Every failure path answers that
// way, so a dashboard that is down, slow or unreachable degrades to exactly the
// behaviour a session has without the hook installed.
func (h *Handler) PermissionRequest(w http.ResponseWriter, r *http.Request) {
	if !h.requireSecret(w, r) {
		return
	}
	var body preToolPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPermissionBodyBytes)).Decode(&body); err != nil {
		writeNoDecision(w)
		return
	}
	if body.SessionID == "" || body.ToolName == "" {
		writeNoDecision(w)
		return
	}

	decision, ok := h.permissions.hold(r.Context(), body)
	if !ok {
		writeNoDecision(w)
		return
	}
	writeDecision(w, decision)
}

// PermissionNotify handles POST /api/hooks/notification — the Notification hook.
// It records that a session's own terminal prompt is up, which is the state the
// dashboard can report but not resolve.
func (h *Handler) PermissionNotify(w http.ResponseWriter, r *http.Request) {
	if !h.requireSecret(w, r) {
		return
	}
	var body notificationPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPermissionBodyBytes)).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if body.NotificationType == permissionPromptNotification && body.SessionID != "" {
		h.permissions.noteTerminalPrompt(body.SessionID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// PermissionRespond handles POST /api/hooks/permission/respond — the browser.
//
// Authenticated by the session-auth group in router.go, not by the hooks bearer
// secret: this call carries the dashboard's session cookie, the same split the
// edit gate already uses.
func (h *Handler) PermissionRespond(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID       string `json:"id"`
		Decision string `json:"decision"` // "allow" | "deny"
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPermissionBodyBytes)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Decision != "allow" && body.Decision != "deny" {
		writeJSONError(w, http.StatusBadRequest, `decision must be "allow" or "deny"`)
		return
	}
	if !h.permissions.resolve(body.ID, body.Decision) {
		// Already lapsed into the terminal prompt, or answered by someone else.
		writeJSONError(w, http.StatusConflict, "this request is no longer waiting for a decision")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// hold registers the request and blocks until a decision arrives, the hold
// lapses, or the hook hangs up. The bool reports whether a decision was made.
func (b *PermissionBridge) hold(ctx context.Context, p preToolPayload) (string, bool) {
	id := uuid.New().String()
	entry := &permissionRequest{
		perm: sdk.PendingPermission{
			ID:          id,
			Tool:        p.ToolName,
			Pattern:     patternOf(p),
			Reason:      nil,
			RequestedAt: b.nowFn().UTC().Format(time.RFC3339),
		},
		sessionID: p.SessionID,
		toolUseID: p.ToolUseID,
		ch:        make(chan string, 1),
	}

	b.mu.Lock()
	b.pending[id] = entry
	// A held call supersedes an older terminal notice for the same session: the
	// decision is ours again.
	delete(b.notices, p.SessionID)
	b.mu.Unlock()
	b.changed()

	defer func() {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		b.changed()
	}()

	select {
	case decision := <-entry.ch:
		return decision, true
	case <-time.After(b.holdFor):
		return "", false
	case <-ctx.Done():
		return "", false
	}
}

func (b *PermissionBridge) resolve(id, decision string) bool {
	b.mu.Lock()
	entry, ok := b.pending[id]
	b.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case entry.ch <- decision:
		return true
	default:
		return false
	}
}

func (b *PermissionBridge) noteTerminalPrompt(sessionID string) {
	b.mu.Lock()
	b.notices[sessionID] = permissionNotice{at: b.nowFn()}
	b.mu.Unlock()
	b.changed()
}

// PendingForSession returns the permission requests currently answerable from
// the dashboard for one session, plus whether that session's own terminal is
// showing a prompt nobody answered here.
func (b *PermissionBridge) PendingForSession(sessionID string) ([]sdk.PendingPermission, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	var out []sdk.PendingPermission
	for _, e := range b.pending {
		if e.sessionID == sessionID {
			out = append(out, e.perm)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RequestedAt < out[j].RequestedAt })

	notice, ok := b.notices[sessionID]
	if ok && b.nowFn().Sub(notice.at) > permissionNoticeTTL {
		delete(b.notices, sessionID)
		ok = false
	}
	return out, ok
}

// patternOf extracts the tool's own argument from the hook payload: the Bash
// command, or the file path for a tool that names one. This is the same value
// the parser derives from the transcript, but taken from the producer instead
// of reconstructed — so it is the argument the session is actually asking about.
func patternOf(p preToolPayload) *string {
	var in struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
		URL      string `json:"url"`
	}
	if json.Unmarshal(p.ToolInput, &in) != nil {
		return nil
	}
	v := in.Command
	if v == "" {
		v = in.FilePath
	}
	if v == "" {
		v = in.URL
	}
	if v == "" {
		return nil
	}
	if len(v) > maxPatternBytes {
		v = v[:maxPatternBytes]
	}
	return &v
}

func writeNoDecision(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	// An empty object, not an error status: the hook prints whatever it gets and
	// an empty object is Claude Code's "carry on as usual".
	_, _ = w.Write([]byte("{}\n"))
}

func writeDecision(w http.ResponseWriter, decision string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"hookSpecificOutput": map[string]string{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       decision,
			"permissionDecisionReason": "decided in the agent dashboard",
		},
	})
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
