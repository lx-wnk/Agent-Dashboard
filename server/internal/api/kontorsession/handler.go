// Package kontorsession serves the Kontor session's HTTP surface.
package kontorsession

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/lx-wnk/kontor/server/internal/apierr"
)

// sessions is the test seam over *kontorsession.Service.
type sessions interface {
	Current(ctx context.Context) (int, bool, error)
	Start(ctx context.Context, prompt string) (int, error)
	Renew(ctx context.Context, prompt string) (int, error)
	End(ctx context.Context, reason string) error
}

type Handler struct{ svc sessions }

func New(s sessions) *Handler { return &Handler{svc: s} }

func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/kontor-session", apierr.ErrorMiddleware(h.get))
	r.Post("/api/kontor-session", apierr.ErrorMiddleware(h.start))
	r.Post("/api/kontor-session/renew", apierr.ErrorMiddleware(h.renew))
	r.Delete("/api/kontor-session", apierr.ErrorMiddleware(h.end))
}

type view struct {
	PID *int `json:"pid"`
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	pid, ok, err := h.svc.Current(r.Context())
	if err != nil {
		return err
	}
	if !ok {
		apierr.WriteJSON(w, http.StatusOK, view{})
		return nil
	}
	apierr.WriteJSON(w, http.StatusOK, view{PID: &pid})
	return nil
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) error {
	return h.spawn(w, r, h.svc.Start, true)
}

func (h *Handler) renew(w http.ResponseWriter, r *http.Request) error {
	return h.spawn(w, r, h.svc.Renew, false)
}

func (h *Handler) spawn(w http.ResponseWriter, r *http.Request, fn func(context.Context, string) (int, error), promptRequired bool) error {
	var body struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if promptRequired && strings.TrimSpace(body.Prompt) == "" {
		return apierr.NewAppError(http.StatusBadRequest, "prompt is required")
	}
	pid, err := fn(r.Context(), body.Prompt)
	if err != nil {
		return apierr.NewAppError(http.StatusInternalServerError, "kontor session: "+err.Error())
	}
	apierr.WriteJSON(w, http.StatusOK, view{PID: &pid})
	return nil
}

func (h *Handler) end(w http.ResponseWriter, r *http.Request) error {
	if err := h.svc.End(r.Context(), "ended by operator"); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
