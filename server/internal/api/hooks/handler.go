// Package hooks implements the /api/hooks route group.
//
// Routes:
//   - POST /api/hooks/event   — receives Claude Code hook events, triggers debounced SSE rescan
//   - POST /api/hooks/pre-tool — edit gate: holds a tool call pending user approval
//   - POST /api/hooks/respond  — approve or reject a pending edit gate decision
//   - GET  /api/hooks/pending  — list pending edit gate decisions
package hooks

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/permissions"
)

const (
	editGateTimeout = 30 * time.Second
)

// OnEventFn is called when an authenticated hook event is received.
type OnEventFn func()

// PendingEdit holds an in-flight edit gate decision.
type PendingEdit struct {
	ID         string `json:"id"`
	SessionID  string `json:"sessionId"`
	ToolName   string `json:"toolName"`
	FilePath   string `json:"filePath"`
	OldContent string `json:"oldContent"`
	NewContent string `json:"newContent"`
	CreatedAt  int64  `json:"createdAt"` // unix ms
	Decision   string `json:"decision"`  // "pending" | "accept" | "reject"
}

// Handler handles /api/hooks requests.
type Handler struct {
	secret  string
	onEvent OnEventFn

	mu      sync.Mutex
	pending map[string]*pendingEntry
}

type pendingEntry struct {
	edit PendingEdit
	ch   chan string // receives "accept" or "reject"
}

// New creates a Handler with the given shared secret and rescan callback.
// When secret is non-empty it is required for all hook endpoints. When empty,
// /api/hooks/event is open (loopback-only by network binding) but PreTool,
// Respond, and Pending still require the secret to prevent unauthenticated
// tool-gate manipulation.
func New(secret string, onEvent OnEventFn) *Handler {
	return &Handler{
		secret:  secret,
		onEvent: onEvent,
		pending: make(map[string]*pendingEntry),
	}
}

// Event handles POST /api/hooks/event.
// Validates the optional bearer secret, acknowledges 204, and triggers a debounced rescan.
func (h *Handler) Event(w http.ResponseWriter, r *http.Request) {
	if h.secret != "" {
		got := bearerToken(r)
		if subtle.ConstantTimeCompare([]byte(got), []byte(h.secret)) != 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
	if h.onEvent != nil {
		go h.onEvent()
	}
}

func (h *Handler) requireSecret(w http.ResponseWriter, r *http.Request) bool {
	if h.secret == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "DASHBOARD_HOOKS_SECRET must be set to use the edit gate"})
		return false
	}
	got := bearerToken(r)
	if subtle.ConstantTimeCompare([]byte(got), []byte(h.secret)) != 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return false
	}
	return true
}

// PreTool handles POST /api/hooks/pre-tool.
// Registers a pending edit decision and blocks until the user approves/rejects or the timeout fires.
func (h *Handler) PreTool(w http.ResponseWriter, r *http.Request) {
	if !h.requireSecret(w, r) {
		return
	}
	var body struct {
		SessionID  string `json:"sessionId"`
		ToolName   string `json:"toolName"`
		FilePath   string `json:"filePath"`
		OldContent string `json:"oldContent"`
		NewContent string `json:"newContent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	// Only gate write-type tools. The canonical set is permissions.IsWriteTool —
	// do not add names here; update the source of truth in permissions/allowlist.go instead.
	if !permissions.IsWriteTool(body.ToolName) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"proceed": true})
		return
	}

	id := uuid.New().String()
	entry := &pendingEntry{
		edit: PendingEdit{
			ID:         id,
			SessionID:  body.SessionID,
			ToolName:   body.ToolName,
			FilePath:   body.FilePath,
			OldContent: body.OldContent,
			NewContent: body.NewContent,
			CreatedAt:  time.Now().UnixMilli(),
			Decision:   "pending",
		},
		ch: make(chan string, 1),
	}

	h.mu.Lock()
	h.pending[id] = entry
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
	}()

	select {
	case decision := <-entry.ch:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"proceed": decision == "accept"})
	case <-time.After(editGateTimeout):
		// Timeout: fail-closed — secret is always set here (requireSecret guards entry).
		proceed := false
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"proceed": proceed})
	case <-r.Context().Done():
		// Client disconnected.
	}
}

// Respond handles POST /api/hooks/respond.
// Sends the approve/reject decision to the waiting pre-tool handler.
func (h *Handler) Respond(w http.ResponseWriter, r *http.Request) {
	if !h.requireSecret(w, r) {
		return
	}
	var body struct {
		ID       string `json:"id"`
		Decision string `json:"decision"` // "accept" | "reject"
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Decision != "accept" && body.Decision != "reject" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "decision must be accept or reject"})
		return
	}

	h.mu.Lock()
	entry, ok := h.pending[body.ID]
	h.mu.Unlock()
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "no pending edit with that id"})
		return
	}

	select {
	case entry.ch <- body.Decision:
	default:
		// Already decided (shouldn't normally happen).
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// Pending handles GET /api/hooks/pending.
// Returns all pending edit gate decisions, optionally filtered by sessionId.
func (h *Handler) Pending(w http.ResponseWriter, r *http.Request) {
	if !h.requireSecret(w, r) {
		return
	}
	sessionID := r.URL.Query().Get("sessionId")

	h.mu.Lock()
	var edits []PendingEdit
	for _, entry := range h.pending {
		if sessionID == "" || entry.edit.SessionID == sessionID {
			edits = append(edits, entry.edit)
		}
	}
	h.mu.Unlock()

	if edits == nil {
		edits = []PendingEdit{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"edits": edits})
}

func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && auth[:7] == "Bearer " {
		return auth[7:]
	}
	return ""
}
