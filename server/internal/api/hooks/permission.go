package hooks

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/sanitize"
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

	// armedTTL bounds how long a session stays armed without being renewed. An
	// arm that is forgotten would otherwise keep holding that session's tool
	// calls for the life of the process.
	armedTTL = 30 * time.Minute

	// maxHoldsPerSession caps concurrent holds for one session. Past the cap the
	// answer is "no decision", which is the same fail-safe a lapse gives, so the
	// overflow degrades to a terminal prompt rather than to unbounded goroutines.
	maxHoldsPerSession = 8

	maxPermissionBodyBytes = 64 * 1024
	// maxPatternRunes bounds the tool argument taken from the hook payload. The
	// value is agent-authored and is displayed next to an approve control, so it
	// is bounded for the eye rather than for the wire.
	maxPatternRunes = 400
)

// permissionRequest is one PreToolUse call held open while a human decides.
type permissionRequest struct {
	perm      sdk.PendingPermission
	sessionID string
	toolUseID string
	// seq orders holds within one session. RequestedAt has second precision, so
	// two calls from the same batch would otherwise sort arbitrarily and the
	// card could answer a different request than the one it rendered.
	seq uint64
	ch  chan string // receives "allow" or "deny"
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
	mu      sync.Mutex
	pending map[string]*permissionRequest // request id -> held call
	notices map[string]permissionNotice   // session id -> terminal is asking
	// armed holds the sessions whose prompts the dashboard should intercept,
	// with the time each was armed. PreToolUse fires before Claude Code decides
	// whether to prompt at all, so it carries no signal that a decision is
	// pending -- holding every call would stall every session on the machine.
	// A session is held only after someone asked for it.
	armed map[string]time.Time
	// seq orders holds within a session; see permissionRequest.seq.
	seq      atomic.Uint64
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
		armed:    map[string]time.Time{},
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

// PermissionArm handles POST /api/hooks/permission/arm — the browser.
//
// Arming is what makes the bridge hold anything at all. PreToolUse fires before
// Claude Code evaluates whether to prompt, so it cannot say "a decision is
// pending"; without an explicit opt-in the bridge would have to hold every tool
// call of every session to catch the few that matter.
func (h *Handler) PermissionArm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID string `json:"sessionId"`
		Armed     bool   `json:"armed"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPermissionBodyBytes)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.SessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "sessionId is required")
		return
	}
	h.permissions.Arm(body.SessionID, body.Armed)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"armed": body.Armed})
}

// hold registers the request and blocks until a decision arrives, the hold
// lapses, or the hook hangs up. The bool reports whether a decision was made.
func (b *PermissionBridge) hold(ctx context.Context, p preToolPayload) (string, bool) {
	seq := b.seq.Add(1)
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
		seq:       seq,
		ch:        make(chan string, 1),
	}

	b.mu.Lock()
	if !b.isArmedLocked(p.SessionID) || b.holdsForLocked(p.SessionID) >= maxHoldsPerSession {
		b.mu.Unlock()
		return "", false
	}
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

// resolve delivers a decision to a held call. Look-up, removal and send happen
// in one critical section: releasing the lock before the send let a hold time
// out in the gap, so the send landed in an orphaned buffer while the caller was
// told the decision had been delivered.
func (b *PermissionBridge) resolve(id, decision string) bool {
	b.mu.Lock()
	entry, ok := b.pending[id]
	if ok {
		delete(b.pending, id)
	}
	b.mu.Unlock()
	if !ok {
		return false
	}
	// Cap-1 buffer, sole sender, entry now unreachable to any other resolver.
	entry.ch <- decision
	return true
}

// Arm marks a session's prompts as ones the dashboard should intercept, or
// clears that mark. Arming is per session and expires: see armedTTL.
func (b *PermissionBridge) Arm(sessionID string, armed bool) {
	b.mu.Lock()
	if armed {
		b.armed[sessionID] = b.nowFn()
	} else {
		delete(b.armed, sessionID)
	}
	b.mu.Unlock()
	b.changed()
}

func (b *PermissionBridge) isArmedLocked(sessionID string) bool {
	at, ok := b.armed[sessionID]
	if !ok {
		return false
	}
	if b.nowFn().Sub(at) > armedTTL {
		delete(b.armed, sessionID)
		return false
	}
	return true
}

func (b *PermissionBridge) holdsForLocked(sessionID string) int {
	n := 0
	for _, e := range b.pending {
		if e.sessionID == sessionID {
			n++
		}
	}
	return n
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
// StateForSession is a pure read: it never mutates the bridge. Expiry is done
// by SweepExpired, called from the same tick that scans agents -- driving it
// from a getter made it observation-driven, so a session that raised a prompt
// and then exited kept its notice for the life of the process.
// Returns the requests answerable right now in arrival order, whether the
// session is showing its own prompt instead, and whether it is armed.
func (b *PermissionBridge) StateForSession(sessionID string) (held []sdk.PendingPermission, atTerminal, armed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	type ordered struct {
		perm sdk.PendingPermission
		seq  uint64
	}
	var hs []ordered
	for _, e := range b.pending {
		if e.sessionID == sessionID {
			hs = append(hs, ordered{perm: e.perm, seq: e.seq})
		}
	}
	// By arrival, not by RequestedAt: that has second precision, so a batch of
	// parallel tool calls would sort arbitrarily and the card could answer a
	// different request than the one it rendered.
	sort.Slice(hs, func(i, j int) bool { return hs[i].seq < hs[j].seq })
	out := make([]sdk.PendingPermission, len(hs))
	for i, h := range hs {
		out[i] = h.perm
	}

	notice, hasNotice := b.notices[sessionID]
	return out, hasNotice && b.nowFn().Sub(notice.at) <= permissionNoticeTTL, b.isArmedReadLocked(sessionID)
}

// SweepExpired drops armed marks and terminal notices that have aged out. It is
// the only place either map shrinks on a timer, so it must be called
// periodically -- the agent scan tick does.
func (b *PermissionBridge) SweepExpired() {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.nowFn()
	for id, at := range b.armed {
		if now.Sub(at) > armedTTL {
			delete(b.armed, id)
		}
	}
	for id, n := range b.notices {
		if now.Sub(n.at) > permissionNoticeTTL {
			delete(b.notices, id)
		}
	}
}

// isArmedReadLocked is the non-mutating half of isArmedLocked, for the read path.
func (b *PermissionBridge) isArmedReadLocked(sessionID string) bool {
	at, ok := b.armed[sessionID]
	return ok && b.nowFn().Sub(at) <= armedTTL
}

// patternOf extracts the tool's own argument from the hook payload: the Bash
// command, the file path for a tool that names one, or the URL. This is the same
// value the parser derives from the transcript, but taken from the producer
// instead of reconstructed — so it is the argument the session is actually
// asking about.
//
// It is sanitized, unlike the parser's PendingToolUse.Pattern. That one is the
// grant identity, matched against a stored preset by exact equality, so
// normalising it would fold two distinct commands onto one rule. This one is
// display-only: the client posts {id, decision} and never sends the pattern
// anywhere. Leaving it raw put agent-authored text with a possible bidi override
// straight into the title of the Allow button — the same gap this project
// closed for the transcript path one layer up.
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
	// Rune-capped by the sanitizer, not sliced by bytes: a cut through a
	// multi-byte character yields U+FFFD on the wire.
	display, _ := sanitize.ForDisplayCapped(v, maxPatternRunes)
	if display == "" {
		return nil
	}
	return &display
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
