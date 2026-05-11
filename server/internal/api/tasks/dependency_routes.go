package tasks

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func (h *Handler) listDependencies(w http.ResponseWriter, r *http.Request) error {
	taskID := chi.URLParam(r, "id")
	deps, err := h.depRepo.ListUpstream(r.Context(), taskID)
	if err != nil {
		return fmt.Errorf("dependencies.list: %w", err)
	}
	if deps == nil {
		deps = []*ent.TaskDependency{}
	}
	return jsonReply(w, http.StatusOK, deps)
}

func (h *Handler) listDependents(w http.ResponseWriter, r *http.Request) error {
	taskID := chi.URLParam(r, "id")
	deps, err := h.depRepo.ListDownstream(r.Context(), taskID)
	if err != nil {
		return fmt.Errorf("dependents.list: %w", err)
	}
	if deps == nil {
		deps = []*ent.TaskDependency{}
	}
	return jsonReply(w, http.StatusOK, deps)
}

func (h *Handler) addDependency(w http.ResponseWriter, r *http.Request) error {
	taskID := chi.URLParam(r, "id")
	var body struct {
		DependsOnID    string `json:"dependsOnId"`
		RequiredStage  string `json:"requiredStage"`
		OnCancelAction string `json:"onCancelAction"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if body.DependsOnID == "" {
		return apierr.NewAppError(http.StatusBadRequest, "dependsOnId is required")
	}
	requiredStage := body.RequiredStage
	if requiredStage == "" {
		requiredStage = "done"
	}
	onCancelAction := body.OnCancelAction
	if onCancelAction == "" {
		onCancelAction = "nothing"
	}
	dep, err := h.depRepo.Add(r.Context(), taskID, body.DependsOnID, requiredStage, onCancelAction)
	if err != nil {
		return fmt.Errorf("dependencies.add: %w", err)
	}
	return jsonReply(w, http.StatusCreated, dep)
}

func (h *Handler) removeDependency(w http.ResponseWriter, r *http.Request) error {
	depID := chi.URLParam(r, "depId")
	if err := h.depRepo.RemoveByID(r.Context(), depID); err != nil {
		if ent.IsNotFound(err) {
			return apierr.ErrNotFound
		}
		return fmt.Errorf("dependencies.remove: %w", err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Ensure depRepo is referenced — keeps the compiler happy if it would otherwise be unused.
var _ repo.DependencyRepo = (*struct{ repo.DependencyRepo })(nil)
