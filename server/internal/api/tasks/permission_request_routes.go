package tasks

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/permissions"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

// listPermissionRequests returns pending permission requests across all stage_runs for a task.
func (h *Handler) listPermissionRequests(w http.ResponseWriter, r *http.Request) error {
	taskID := chi.URLParam(r, "id")
	runs, err := h.srRepo.ListForTask(r.Context(), taskID)
	if err != nil {
		return fmt.Errorf("permission_requests.list: %w", err)
	}
	if len(runs) == 0 {
		return jsonReply(w, http.StatusOK, []*ent.PermissionRequest{})
	}
	ids := make([]string, len(runs))
	for i, sr := range runs {
		ids[i] = sr.ID
	}
	reqs, err := h.permRepo.ListPendingForTask(r.Context(), taskID, ids)
	if err != nil {
		return fmt.Errorf("permission_requests.list: %w", err)
	}
	if reqs == nil {
		reqs = []*ent.PermissionRequest{}
	}
	return jsonReply(w, http.StatusOK, reqs)
}

// createPermissionRequest creates a single permission request and flips the stage_run to awaiting_user if running.
func (h *Handler) createPermissionRequest(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		StageRunID string  `json:"stageRunId"`
		Tool       string  `json:"tool"`
		Pattern    *string `json:"pattern"`
		Reason     *string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if body.StageRunID == "" || body.Tool == "" {
		return apierr.NewAppError(http.StatusBadRequest, "stageRunId and tool are required")
	}

	req, err := h.permRepo.CreatePermissionRequest(r.Context(), repo.CreatePermissionRequestInput{
		StageRunID: body.StageRunID,
		Tool:       body.Tool,
		Pattern:    body.Pattern,
		Reason:     body.Reason,
	})
	if err != nil {
		return fmt.Errorf("permission_request.create: %w", err)
	}

	// Flip stage_run to awaiting_user if it is currently running.
	sr, err := h.srRepo.GetByID(r.Context(), body.StageRunID)
	if err == nil && sr.Status == "running" {
		awaitingUser := "awaiting_user"
		if _, err2 := h.srRepo.Update(r.Context(), body.StageRunID, repo.UpdateStageRunInput{Status: &awaitingUser}); err2 != nil {
			slog.Warn("createPermissionRequest: flip to awaiting_user failed", "stageRunID", body.StageRunID, "err", err2)
		}
		h.broadcaster.Broadcast(sse.TaskEvent{Type: "task_changed", TaskID: sr.TaskID})
	}

	return jsonReply(w, http.StatusCreated, req)
}

// bulkGrantPermissions handles POST /api/tasks/{id}/permissions/bulk.
func (h *Handler) bulkGrantPermissions(w http.ResponseWriter, r *http.Request) error {
	taskID := chi.URLParam(r, "id")
	var body struct {
		Template    *string `json:"template"`
		Permissions []struct {
			Tool      string  `json:"tool"`
			Pattern   *string `json:"pattern"`
			ExpiresAt *string `json:"expiresAt"`
		} `json:"permissions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}

	var entries []repo.GrantEntry

	// Template grants.
	if body.Template != nil {
		templateEntries, err := resolveTemplate(*body.Template)
		if err != nil {
			return apierr.NewAppError(http.StatusBadRequest, err.Error())
		}
		entries = append(entries, templateEntries...)
	}

	// Explicit grants.
	for _, p := range body.Permissions {
		if p.Tool == "" {
			return apierr.NewAppError(http.StatusBadRequest, "each permission entry requires a tool")
		}
		if !permissions.IsAllowedTool(p.Tool) {
			return apierr.NewAppError(http.StatusBadRequest, "unknown tool: "+p.Tool)
		}
		if p.Tool == "Bash" {
			if p.Pattern == nil || *p.Pattern == "" {
				return apierr.NewAppError(http.StatusBadRequest, "Bash permission requires a non-empty pattern")
			}
			normalized := strings.Join(strings.Fields(*p.Pattern), " ")
			if permissions.DangerousBashRE.MatchString(normalized) {
				return apierr.NewAppError(http.StatusBadRequest, "dangerous Bash pattern rejected")
			}
		}
		e := repo.GrantEntry{Tool: p.Tool, Pattern: p.Pattern}
		if p.ExpiresAt != nil {
			t, err := time.Parse(time.RFC3339, *p.ExpiresAt)
			if err != nil {
				return apierr.NewAppError(http.StatusBadRequest, "expiresAt must be RFC3339")
			}
			e.ExpiresAt = &t
		}
		entries = append(entries, e)
	}

	granted, err := h.permRepo.BulkGrantPermissions(r.Context(), taskID, entries)
	if err != nil {
		return fmt.Errorf("bulk_grant: %w", err)
	}
	if granted == nil {
		granted = []*ent.TaskPermission{}
	}
	h.broadcastEnrichedUpdate(r.Context(), taskID)
	return jsonReply(w, http.StatusOK, granted)
}

// bulkCreatePermissionRequests handles POST /api/permission-requests/bulk.
// Auto-resolves entries already covered by task_permissions; creates rows for the rest.
func (h *Handler) bulkCreatePermissionRequests(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		StageRunID string `json:"stageRunId"`
		Entries    []struct {
			Tool    string  `json:"tool"`
			Pattern *string `json:"pattern"`
			Reason  *string `json:"reason"`
		} `json:"entries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if body.StageRunID == "" {
		return apierr.NewAppError(http.StatusBadRequest, "stageRunId is required")
	}

	sr, err := h.srRepo.GetByID(r.Context(), body.StageRunID)
	if err != nil {
		if ent.IsNotFound(err) {
			return apierr.ErrNotFound
		}
		return fmt.Errorf("bulk_perm_req: get stage run: %w", err)
	}

	// Fetch current effective task permissions to auto-resolve covered entries.
	effectivePerms, err := h.permRepo.ListEffectiveTaskPermissions(r.Context(), sr.TaskID)
	if err != nil {
		return fmt.Errorf("bulk_perm_req: list perms: %w", err)
	}

	type result struct {
		Tool       string  `json:"tool"`
		Pattern    *string `json:"pattern"`
		AutoGranted bool   `json:"autoGranted"`
		RequestID  *string `json:"requestId,omitempty"`
	}

	results := make([]result, 0, len(body.Entries))
	hasNewRequests := false

	for _, e := range body.Entries {
		if e.Tool == "" {
			continue
		}
		if isCovered(e.Tool, e.Pattern, effectivePerms) {
			results = append(results, result{Tool: e.Tool, Pattern: e.Pattern, AutoGranted: true})
			continue
		}
		req, err2 := h.permRepo.CreatePermissionRequest(r.Context(), repo.CreatePermissionRequestInput{
			StageRunID: body.StageRunID,
			Tool:       e.Tool,
			Pattern:    e.Pattern,
			Reason:     e.Reason,
		})
		if err2 != nil {
			return fmt.Errorf("bulk_perm_req: create: %w", err2)
		}
		hasNewRequests = true
		id := req.ID
		results = append(results, result{Tool: e.Tool, Pattern: e.Pattern, AutoGranted: false, RequestID: &id})
	}

	// Flip stage_run to awaiting_user if there are new unresolved requests and it is running.
	if hasNewRequests && sr.Status == "running" {
		awaitingUser := "awaiting_user"
		if _, err2 := h.srRepo.Update(r.Context(), body.StageRunID, repo.UpdateStageRunInput{Status: &awaitingUser}); err2 != nil {
			slog.Warn("bulkCreatePermissionRequests: flip to awaiting_user failed", "stageRunID", body.StageRunID, "err", err2)
		}
		h.broadcaster.Broadcast(sse.TaskEvent{Type: "task_changed", TaskID: sr.TaskID})
	}

	return jsonReply(w, http.StatusOK, results)
}

// bulkResolvePermissionRequests handles POST /api/permission-requests/bulk-resolve.
func (h *Handler) bulkResolvePermissionRequests(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		TaskID        string   `json:"taskId"`
		Decision      string   `json:"decision"`
		PermissionIDs []string `json:"permissionIds"`
		All           bool     `json:"all"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if body.TaskID == "" {
		return apierr.NewAppError(http.StatusBadRequest, "taskId is required")
	}
	if body.Decision != "accept" && body.Decision != "reject" {
		return apierr.NewAppError(http.StatusBadRequest, "decision must be accept or reject")
	}

	outcome := "granted"
	if body.Decision == "reject" {
		outcome = "denied"
	}

	// Collect IDs to resolve.
	var idsToResolve []string
	if body.All {
		runs, err := h.srRepo.ListForTask(r.Context(), body.TaskID)
		if err != nil {
			return fmt.Errorf("bulk_resolve: list runs: %w", err)
		}
		runIDs := make([]string, len(runs))
		for i, sr := range runs {
			runIDs[i] = sr.ID
		}
		pending, err := h.permRepo.ListPendingForTask(r.Context(), body.TaskID, runIDs)
		if err != nil {
			return fmt.Errorf("bulk_resolve: list pending: %w", err)
		}
		for _, req := range pending {
			idsToResolve = append(idsToResolve, req.ID)
		}
	} else {
		idsToResolve = body.PermissionIDs
	}

	var resolveErrors []string
	for _, id := range idsToResolve {
		if err := h.permRepo.ResolvePermissionRequest(r.Context(), id, outcome); err != nil {
			resolveErrors = append(resolveErrors, err.Error())
		}
	}

	// If accepting, grant matching task permissions and resume the task.
	if outcome == "granted" && len(idsToResolve) > 0 {
		if _, err := h.orchestrator.ResumeFromUser(r.Context(), body.TaskID); err != nil {
			slog.Warn("bulk_resolve: ResumeFromUser failed", "taskID", body.TaskID, "err", err)
		}
	}

	h.broadcastEnrichedUpdate(r.Context(), body.TaskID)
	return jsonReply(w, http.StatusOK, map[string]any{
		"resolved": len(idsToResolve),
		"errors":   resolveErrors,
	})
}

// isCovered checks whether a tool+pattern request is already satisfied by effective permissions.
func isCovered(tool string, pattern *string, perms []*ent.TaskPermission) bool {
	for _, p := range perms {
		if p.Tool != tool {
			continue
		}
		// No pattern required — any grant for this tool covers it.
		if pattern == nil {
			return true
		}
		// Pattern required — must match exactly or perm has no pattern restriction.
		if p.Pattern == nil {
			return true
		}
		if *p.Pattern == *pattern {
			return true
		}
	}
	return false
}

// resolveTemplate expands a named permission template to GrantEntry slice.
// Delegates to permissions.ResolveTemplate — the single source of truth for template definitions.
func resolveTemplate(name string) ([]repo.GrantEntry, error) {
	tools, err := permissions.ResolveTemplate(name)
	if err != nil {
		return nil, err
	}
	entries := make([]repo.GrantEntry, len(tools))
	for i, t := range tools {
		entries[i] = repo.GrantEntry{Tool: t}
	}
	return entries, nil
}
