// Package admin serves privileged server-control endpoints (currently restart).
package admin

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Validator decides whether a restart is safe (e.g. won't lock out auth).
type Validator interface {
	Validate(ctx context.Context) error
}

// Handler serves POST /api/admin/restart. trigger signals the run-loop to
// restart; mode is reported in the 202 body.
type Handler struct {
	validator Validator
	mode      string
	trigger   func()
}

func New(v Validator, mode string, trigger func()) *Handler {
	return &Handler{validator: v, mode: mode, trigger: trigger}
}

func (h *Handler) Mount(r chi.Router) {
	r.Post("/api/admin/restart", h.restart)
}

func (h *Handler) restart(w http.ResponseWriter, r *http.Request) {
	if err := h.validator.Validate(r.Context()); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	h.trigger()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "restarting", "mode": h.mode})
}
