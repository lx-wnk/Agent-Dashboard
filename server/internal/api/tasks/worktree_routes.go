package tasks

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

// WorktreeStatusProvider is the subset of services.WorktreeManager the
// handler needs. Keeping it as an interface lets the test layer swap in a
// fake without depending on a real git binary.
type WorktreeStatusProvider interface {
	WorktreeStatus(ctx context.Context, taskID string) (*sdk.WorktreeStatusDTO, error)
}

// getWorktreeStatusHandler answers GET /api/tasks/{id}/worktree with a
// WorktreeStatusDTO, or 204 No Content if the task has no worktree.
func (h *Handler) getWorktreeStatusHandler(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if h.worktreeMgr == nil {
		return apierr.NewAppError(http.StatusServiceUnavailable, "worktree status unavailable")
	}
	dto, err := h.worktreeMgr.WorktreeStatus(r.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			return apierr.ErrNotFound
		}
		return fmt.Errorf("worktree_status: %w", err)
	}
	if dto == nil {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	return jsonReply(w, http.StatusOK, dto)
}
