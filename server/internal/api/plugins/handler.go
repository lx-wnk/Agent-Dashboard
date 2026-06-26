package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	"github.com/lx-wnk/agent-dashboard/server/internal/pluginsctl"
)

// Controller is the behaviour the handler needs from pluginsctl; faked in tests.
type Controller interface {
	List() ([]pluginsctl.PluginState, error)
	SetEnabled(ctx context.Context, id string, enable bool) (pluginsctl.Applied, error)
}

// Handler serves the plugin listing and enable/disable endpoints under
// /api/settings/plugins.
type Handler struct {
	ctl Controller
}

// New creates a Handler backed by the given controller.
func New(ctl Controller) *Handler {
	return &Handler{ctl: ctl}
}

// Mount registers plugin routes on r.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/settings/plugins", apierr.ErrorMiddleware(h.list))
	// Distinct segment to avoid the proxy mount at /api/settings/plugins/{id},
	// which greedily owns that prefix for healthy route/ui extensions.
	r.Patch("/api/settings/plugins-enabled/{id}", apierr.ErrorMiddleware(h.patch))
}

// pluginInfo is intentionally a narrow DTO. Do NOT add Entry/Descriptor fields —
// Descriptor.Env may carry plugin auth secrets and BaseURL must never be exposed
// (F028: leaks internal plugin address — clients must not proxy directly to plugins).
type pluginInfo struct {
	ID           string   `json:"id"`
	Capabilities []string `json:"capabilities"`
	Enabled      bool     `json:"enabled"`
	Healthy      bool     `json:"healthy"`
	AuthProvider bool     `json:"authProvider"`
}

func (h *Handler) list(w http.ResponseWriter, _ *http.Request) error {
	states, err := h.ctl.List()
	if err != nil {
		return fmt.Errorf("plugins.list: %w", err)
	}
	out := make([]pluginInfo, 0, len(states))
	for _, s := range states {
		caps := s.Capabilities
		if caps == nil {
			caps = []string{}
		}
		out = append(out, pluginInfo{
			ID:           s.ID,
			Capabilities: caps,
			Enabled:      s.Enabled,
			Healthy:      s.Healthy,
			AuthProvider: s.AuthProvider,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache") // F034: prevents stale plugin list after restart
	return json.NewEncoder(w).Encode(out)
}

type patchResponse struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	Applied string `json:"applied"`
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if id == "" {
		return fmt.Errorf("%w: plugin id is required", apierr.ErrBadRequest)
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return fmt.Errorf("%w: invalid JSON", apierr.ErrBadRequest)
	}
	applied, err := h.ctl.SetEnabled(r.Context(), id, body.Enabled)
	if err != nil {
		if errors.Is(err, pluginsctl.ErrUnknownPlugin) {
			return fmt.Errorf("%w: unknown plugin %q", apierr.ErrBadRequest, id)
		}
		return fmt.Errorf("plugins.patch: %w", err)
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(patchResponse{ID: id, Enabled: body.Enabled, Applied: string(applied)})
}
