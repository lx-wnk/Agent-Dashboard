// Package systemprompts implements CRUD endpoints for custom system prompts
// at /api/settings/system-prompts.
package systemprompts

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// Handler handles the /api/settings/system-prompts endpoints.
type Handler struct {
	repo repo.SystemPromptRepo
}

// NewHandler creates a Handler backed by the given repo.
func NewHandler(r repo.SystemPromptRepo) *Handler {
	return &Handler{repo: r}
}

// Mount registers all system-prompt routes on r.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/settings/system-prompts", apierr.ErrorMiddleware(h.list))
	r.Post("/api/settings/system-prompts", apierr.ErrorMiddleware(h.create))
	r.Put("/api/settings/system-prompts/{id}", apierr.ErrorMiddleware(h.update))
	r.Delete("/api/settings/system-prompts/{id}", apierr.ErrorMiddleware(h.delete))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	prompts, err := h.repo.List(r.Context())
	if err != nil {
		return err
	}
	if prompts == nil {
		prompts = []*ent.SystemPrompt{}
	}
	apierr.WriteJSON(w, http.StatusOK, prompts)
	return nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	var in repo.CreateSystemPromptInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if in.Content == "" {
		return apierr.NewAppError(http.StatusBadRequest, "content is required")
	}
	prompt, err := h.repo.Create(r.Context(), in)
	if err != nil {
		return err
	}
	apierr.WriteJSON(w, http.StatusCreated, prompt)
	return nil
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	var in repo.UpdateSystemPromptInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	prompt, err := h.repo.Update(r.Context(), id, in)
	if err != nil {
		return err
	}
	apierr.WriteJSON(w, http.StatusOK, prompt)
	return nil
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if err := h.repo.Delete(r.Context(), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
