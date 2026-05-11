package tasks

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
)

// resumeStage SIGTERMs any dead run on the current stage and re-progresses the task.
func (h *Handler) resumeStage(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := h.taskRepo.GetByID(r.Context(), id); err != nil {
		return apierr.ErrNotFound
	}
	sr, err := h.orchestrator.ResumeFromUser(r.Context(), id)
	if err != nil {
		return fmt.Errorf("resume_stage: %w", err)
	}
	if sr == nil {
		return apierr.NewAppError(http.StatusConflict, "task cannot be resumed (terminal, missing, or slot full)")
	}
	h.broadcastEnrichedUpdate(r.Context(), id)
	return jsonReply(w, http.StatusOK, sr)
}
