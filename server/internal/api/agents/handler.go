// Package agents provides HTTP handlers for the agent monitoring API.
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

// GetAgentsFn is the function signature for retrieving the current agent list.
// Defined as a named type to allow substitution in tests.
type GetAgentsFn func(ctx context.Context) ([]sdk.Agent, error)

// Handler handles /api/agents HTTP requests.
type Handler struct {
	getAgents   GetAgentsFn
	broadcaster *sse.Broadcaster
}

// NewHandler creates a Handler with the given dependencies.
func NewHandler(getAgents GetAgentsFn, broadcaster *sse.Broadcaster) *Handler {
	return &Handler{getAgents: getAgents, broadcaster: broadcaster}
}

// List handles GET /api/agents — returns the current agent list as JSON.
// A nil slice is normalized to an empty slice so the frontend always receives [].
func (h *Handler) List(w http.ResponseWriter, r *http.Request) error {
	agents, err := h.getAgents(r.Context())
	if err != nil {
		return fmt.Errorf("get agents: %w", err)
	}
	if agents == nil {
		agents = []sdk.Agent{}
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(agents)
}

// Stream handles GET /api/agents/stream — SSE endpoint.
// Sends the current agent list immediately, then on every broadcaster tick.
func (h *Handler) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	sse.WriteHeaders(w)

	// Send current state immediately so client doesn't wait for first tick.
	// Wrap in {agents, trend} — the SSE client expects this shape.
	if agents, err := h.getAgents(r.Context()); err == nil {
		if data, err := json.Marshal(map[string]any{"agents": agents, "trend": []any{}}); err == nil {
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}

	sub := h.broadcaster.Subscribe()
	defer h.broadcaster.Unsubscribe(sub)

	for {
		select {
		case data, ok := <-sub:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
