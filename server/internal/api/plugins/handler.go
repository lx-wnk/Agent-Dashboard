package plugins

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

// Handler serves GET /api/plugins.
type Handler struct {
	reg *plugin.Registry
}

// New creates a Handler backed by the given registry.
func New(reg *plugin.Registry) *Handler {
	return &Handler{reg: reg}
}

// Mount registers plugin routes on r.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/plugins", h.list)
}

// pluginInfo is intentionally a narrow DTO. Do NOT replace with direct Entry/Descriptor
// encoding — Descriptor.Env may contain plugin auth secrets, and BaseURL must not
// be exposed (F028: leaks internal plugin address — clients must not proxy directly to plugins).
type pluginInfo struct {
	ID           string   `json:"id"`
	Capabilities []string `json:"capabilities"`
}

func (h *Handler) list(w http.ResponseWriter, _ *http.Request) {
	infos := h.reg.Infos()
	out := make([]pluginInfo, 0, len(infos))
	for _, info := range infos {
		out = append(out, pluginInfo{
			ID:           info.ID,
			Capabilities: info.Capabilities,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache") // F034: prevents stale plugin list after restart
	_ = json.NewEncoder(w).Encode(out)
}
