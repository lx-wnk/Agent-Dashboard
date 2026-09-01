// Package obsidian implements the HTTP surface that triggers an Obsidian
// vault indexing pass on demand.
package obsidian

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	obsidianapp "github.com/lx-wnk/agent-dashboard/server/internal/apps/obsidian"
	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/memory"
)

// Handler serves POST /api/obsidian/index, the manual trigger for
// obsidianapp.IndexNotes.
type Handler struct {
	client  *obsidianapp.Client
	mem     repo.MemoryRepo
	gate    memory.Gate
	spaceID string
}

// NewHandler creates a Handler. client is nil when the vault is unconfigured
// (see serverapp.buildObsidianClient's own doc comment) — index then answers
// 503 rather than reaching a nil client, the same "optional integration,
// never a boot failure" rule that function follows.
func NewHandler(client *obsidianapp.Client, mem repo.MemoryRepo, gate memory.Gate, spaceID string) *Handler {
	return &Handler{client: client, mem: mem, gate: gate, spaceID: spaceID}
}

// Mount registers the /api/obsidian/* routes on r.
func (h *Handler) Mount(r chi.Router) {
	r.Post("/api/obsidian/index", apierr.ErrorMiddleware(h.index))
}

// index runs one obsidianapp.IndexNotes pass and reports how many new
// pointer entries it created.
//
// A human presses the button that reaches this handler, but the run behind
// it is unattended with respect to capabilities: h.gate is built with no
// Asker (see its construction in serverapp/di.go), so a capability that
// would otherwise pause for a human's decision denies instead — the click
// only starts the run, it is not present to answer for it. IndexNotes'
// three internal capability checks can therefore fail in two different ways
// depending on whether the capability class defaults to deny or ask
// (capability.Decide's defaultEffect); both capability.ErrDenied and
// capability.ErrAskRequired mean the same thing to this handler's caller —
// forbidden — so both map to 403, never the default 500 ErrorMiddleware
// would otherwise give an unrecognised error.
func (h *Handler) index(w http.ResponseWriter, r *http.Request) error {
	if h.client == nil {
		return apierr.NewAppError(http.StatusServiceUnavailable, "obsidian vault not configured")
	}
	count, err := obsidianapp.IndexNotes(r.Context(), h.client, h.mem, h.gate, h.spaceID)
	if err != nil {
		if errors.Is(err, capability.ErrDenied) || errors.Is(err, capability.ErrAskRequired) {
			return apierr.NewAppError(http.StatusForbidden, err.Error())
		}
		return err
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]int{"indexed": count})
	return nil
}
