// Package capabilities implements the browser-facing endpoint that lets a
// human answer a pending capability decision raised by serverask.Asker.
package capabilities

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	"github.com/lx-wnk/agent-dashboard/server/internal/askgate"
	"github.com/lx-wnk/agent-dashboard/server/internal/serverask"
)

// maxRespondBodyBytes mirrors hooks.maxPermissionBodyBytes, which answers the
// same shape of request but is unexported to that package.
const maxRespondBodyBytes = 64 * 1024

// Resolver is the one method Handler needs from *serverask.Asker.
type Resolver interface {
	Resolve(id, decision string) error
}

// Handler answers POST /api/capabilities/decisions/respond.
type Handler struct {
	resolver Resolver
}

// New returns a Handler that delivers decisions through resolver.
func New(resolver Resolver) *Handler {
	return &Handler{resolver: resolver}
}

// Respond handles POST /api/capabilities/decisions/respond — the browser.
//
// Authenticated by the session-auth group in router.go, the same split
// hooks.PermissionRespond uses.
func (h *Handler) Respond(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID       string `json:"id"`
		Decision string `json:"decision"` // "allow" | "deny"
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRespondBodyBytes)).Decode(&body); err != nil {
		apierr.JSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Decision != "allow" && body.Decision != "deny" {
		apierr.JSONError(w, http.StatusBadRequest, `decision must be "allow" or "deny"`)
		return
	}
	switch err := h.resolver.Resolve(body.ID, body.Decision); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, askgate.ErrNotPending):
		apierr.JSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, serverask.ErrInvalidDecision):
		apierr.JSONError(w, http.StatusBadRequest, err.Error())
	default:
		apierr.JSONError(w, http.StatusInternalServerError, "internal error")
	}
}
