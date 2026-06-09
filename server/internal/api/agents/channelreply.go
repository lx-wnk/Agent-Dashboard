package agents

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
)

const maxRepliesPerPID = 50

// Reply is a single message received from a channel bridge.
type Reply struct {
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// ReplyStore is a thread-safe ring-buffer store for channel replies, keyed by parent PID.
type ReplyStore struct {
	mu    sync.RWMutex
	store map[int][]Reply
}

// NewReplyStore returns an empty ReplyStore.
func NewReplyStore() *ReplyStore {
	return &ReplyStore{store: make(map[int][]Reply)}
}

// Add appends a reply for parentPid, evicting the oldest entry when the ring is full.
func (s *ReplyStore) Add(parentPid int, message, timestamp string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	replies := s.store[parentPid]
	replies = append(replies, Reply{Message: message, Timestamp: timestamp})
	if len(replies) > maxRepliesPerPID {
		replies = replies[len(replies)-maxRepliesPerPID:]
	}
	s.store[parentPid] = replies
}

// Since returns all replies for parentPid received after the given RFC3339 timestamp.
// If since is empty, all stored replies are returned.
func (s *ReplyStore) Since(parentPid int, since string) []Reply {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := s.store[parentPid]
	if since == "" || len(all) == 0 {
		cp := make([]Reply, len(all))
		copy(cp, all)
		return cp
	}
	sinceTime, err := time.Parse(time.RFC3339, since)
	if err != nil {
		cp := make([]Reply, len(all))
		copy(cp, all)
		return cp
	}
	var out []Reply
	for _, r := range all {
		t, err := time.Parse(time.RFC3339, r.Timestamp)
		if err != nil {
			continue
		}
		if t.After(sinceTime) {
			out = append(out, r)
		}
	}
	return out
}

// ChannelReplyHandler handles the /api/channel-reply endpoint.
type ChannelReplyHandler struct {
	store   *ReplyStore
	apiKeys repo.ApiKeyRepo
}

// NewChannelReplyHandler creates a handler backed by the given store. apiKeys
// authenticates the bridge's bearer token (the MCP api_keys token it already
// sends on every outbound call) — the same mechanism /api/mcp uses.
func NewChannelReplyHandler(store *ReplyStore, apiKeys repo.ApiKeyRepo) *ChannelReplyHandler {
	return &ChannelReplyHandler{store: store, apiKeys: apiKeys}
}

// Post handles POST /api/channel-reply.
// The channel bridge posts here to store agent replies on behalf of a parent PID.
// Auth: the bearer token is the MCP api_keys token the bridge sends on every
// outbound call (DASHBOARD_MCP_TOKEN), validated by SHA-256 hash lookup — the
// same mechanism /api/mcp uses. (The per-PID discovery-file token is for the
// INBOUND dashboard→agent direction only and is never sent on this call.)
func (h *ChannelReplyHandler) Post(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ParentPid int    `json:"parentPid"`
		Message   string `json:"message"`
		Timestamp string `json:"timestamp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if body.ParentPid <= 0 || body.Message == "" || body.Timestamp == "" {
		http.Error(w, `{"error":"missing required fields"}`, http.StatusBadRequest)
		return
	}

	if h.apiKeys == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	hash := mcp.HashToken(bearerToken(r))
	if _, err := h.apiKeys.GetByHash(r.Context(), hash); err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	h.store.Add(body.ParentPid, body.Message, body.Timestamp)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// GetReplies handles GET /api/agents/{pid}/replies.
func (h *ChannelReplyHandler) GetReplies(w http.ResponseWriter, r *http.Request) {
	pidStr := r.PathValue("pid")
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		http.Error(w, `{"error":"invalid pid"}`, http.StatusBadRequest)
		return
	}
	since := r.URL.Query().Get("since")
	replies := h.store.Since(pid, since)
	if replies == nil {
		replies = []Reply{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(replies)
}

func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && auth[:7] == "Bearer " {
		return auth[7:]
	}
	return ""
}
