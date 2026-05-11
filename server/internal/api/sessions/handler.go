// Package sessions provides HTTP handlers for session listing and output reading.
package sessions

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

var sessionIDRE = regexp.MustCompile(`(?i)^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

// List handles GET /api/sessions.
// Returns the 100 most recently modified sessions with token/model/preview info.
func List(w http.ResponseWriter, r *http.Request) {
	sessions, err := parser.GetSessions(r.Context())
	if err != nil {
		slog.Error("sessions list", "err", err)
		http.Error(w, `{"error":"failed to list sessions"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sessions)
}

// Timeline handles GET /api/sessions/{sessionId}/timeline.
// Returns all tool_call messages with timestamps — used to build a session timeline.
func Timeline(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionId")
	if !sessionIDRE.MatchString(sessionID) {
		http.Error(w, `{"error":"invalid sessionId format"}`, http.StatusBadRequest)
		return
	}

	messages, err := parser.ParseFullSession(sessionID, false)
	if err != nil {
		slog.Error("session timeline", "sessionId", sessionID, "err", err)
		http.Error(w, `{"error":"failed to read session timeline"}`, http.StatusInternalServerError)
		return
	}

	var toolCalls []parser.OutputMessage
	for _, m := range messages {
		if m.Role == "tool_call" && m.Timestamp != nil {
			toolCalls = append(toolCalls, m)
		}
	}
	if toolCalls == nil {
		toolCalls = []parser.OutputMessage{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"toolCalls": toolCalls})
}

// Output handles GET /api/agents/{sessionId}/output.
// Returns all messages from the session JSONL, or just the last assistant message
// when ?last=1 is set.
func Output(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionId")
	if !sessionIDRE.MatchString(sessionID) {
		http.Error(w, `{"error":"invalid sessionId format"}`, http.StatusBadRequest)
		return
	}

	lastOnly := r.URL.Query().Get("last") == "1"
	messages, err := parser.ParseFullSession(sessionID, lastOnly)
	if err != nil {
		slog.Error("session output", "sessionId", sessionID, "err", err)
		http.Error(w, `{"error":"failed to read session output"}`, http.StatusInternalServerError)
		return
	}
	if messages == nil {
		messages = []parser.OutputMessage{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"messages": messages})
}
