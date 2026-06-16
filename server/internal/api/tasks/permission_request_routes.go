package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/permissions"
)

// permissionRequestResponse is the API response shape for a single pending permission request.
// It extends the DB row with computed fields that are not stored in the database.
type permissionRequestResponse struct {
	ID             string  `json:"id"`
	StageRunID     string  `json:"stageRunId"`
	Tool           string  `json:"tool"`
	Pattern        *string `json:"pattern"`
	Reason         *string `json:"reason"`
	Outcome        *string `json:"outcome"`
	RequestedAt    string  `json:"requestedAt"`
	ResolvedAt     *string `json:"resolvedAt"`
	// OutsideSafeList is true when the request is for a Bash command not in the
	// safe allow-list, meaning a Grant is a conscious human override.
	OutsideSafeList bool `json:"outsideSafeList"`
}

func toPermissionRequestResponse(req *ent.PermissionRequest) permissionRequestResponse {
	r := permissionRequestResponse{
		ID:          req.ID,
		StageRunID:  req.StageRunID,
		Tool:        req.Tool,
		Pattern:     req.Pattern,
		Reason:      req.Reason,
		Outcome:     req.Outcome,
		RequestedAt: req.RequestedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if req.ResolvedAt != nil {
		s := req.ResolvedAt.Format("2006-01-02T15:04:05Z07:00")
		r.ResolvedAt = &s
	}
	if req.Tool == "Bash" && req.Pattern != nil {
		normalized := strings.Join(strings.Fields(*req.Pattern), " ")
		if normalized != "" {
			ok, _ := permissions.IsSafeBashPattern(normalized)
			r.OutsideSafeList = !ok
		}
	}
	return r
}

// listPermissionRequests returns pending permission requests across all stage_runs for a task.
func (h *Handler) listPermissionRequests(w http.ResponseWriter, r *http.Request) error {
	taskID := chi.URLParam(r, "id")
	runs, err := h.srRepo.ListForTask(r.Context(), taskID)
	if err != nil {
		return fmt.Errorf("permission_requests.list: %w", err)
	}
	if len(runs) == 0 {
		return jsonReply(w, http.StatusOK, []permissionRequestResponse{})
	}
	ids := make([]string, len(runs))
	for i, sr := range runs {
		ids[i] = sr.ID
	}
	reqs, err := h.permRepo.ListPendingForTask(r.Context(), taskID, ids)
	if err != nil {
		return fmt.Errorf("permission_requests.list: %w", err)
	}
	resp := make([]permissionRequestResponse, len(reqs))
	for i, req := range reqs {
		resp[i] = toPermissionRequestResponse(req)
	}
	return jsonReply(w, http.StatusOK, resp)
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
		h.broadcastEnrichedEvent(r.Context(), "permission_request", sr.TaskID)
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
		pattern := ""
		if p.Pattern != nil {
			pattern = *p.Pattern
		}
		if err := permissions.ValidateGrantEntry(p.Tool, pattern); err != nil {
			return apierr.NewAppError(http.StatusBadRequest, err.Error())
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

	// When a client is available, wrap the grant + audit in a single transaction
	// so a partial failure cannot leave a grant with no forensic trace (or vice-versa).
	var granted []*ent.TaskPermission
	if h.client != nil {
		tx, err := h.client.Tx(r.Context())
		if err != nil {
			return fmt.Errorf("bulk_grant: begin tx: %w", err)
		}
		txCommitted := false
		defer func() {
			if !txCommitted {
				_ = tx.Rollback()
			}
		}()

		txPermRepo := repo.NewPermissionRepo(tx.Client())
		granted, err = txPermRepo.BulkGrantPermissions(r.Context(), taskID, entries)
		if err != nil {
			return fmt.Errorf("bulk_grant: %w", err)
		}
		if granted == nil {
			granted = []*ent.TaskPermission{}
		}

		var userID *string
		if payload, ok := auth.PayloadFromContext(r.Context()); ok && payload.Sub != "" {
			s := payload.Sub
			userID = &s
		}
		tools := make([]string, 0, len(granted))
		for _, p := range granted {
			tools = append(tools, p.Tool)
		}
		txAuditRepo := repo.NewAuditEventRepo(tx.Client())
		if err = txAuditRepo.RecordAudit(r.Context(), userID,
			repo.AuditActionPermissionGrant,
			taskID,
			map[string]any{"tools": tools},
		); err != nil {
			return fmt.Errorf("bulk_grant: record audit: %w", err)
		}

		if err = tx.Commit(); err != nil {
			return fmt.Errorf("bulk_grant: commit: %w", err)
		}
		txCommitted = true
	} else {
		// No client (e.g. tests with mocked repos) — write without tx.
		var err error
		granted, err = h.permRepo.BulkGrantPermissions(r.Context(), taskID, entries)
		if err != nil {
			return fmt.Errorf("bulk_grant: %w", err)
		}
		if granted == nil {
			granted = []*ent.TaskPermission{}
		}
		if h.auditEventRepo != nil {
			var userID *string
			if payload, ok := auth.PayloadFromContext(r.Context()); ok && payload.Sub != "" {
				s := payload.Sub
				userID = &s
			}
			tools := make([]string, 0, len(granted))
			for _, p := range granted {
				tools = append(tools, p.Tool)
			}
			if err = h.auditEventRepo.RecordAudit(r.Context(), userID,
				repo.AuditActionPermissionGrant,
				taskID,
				map[string]any{"tools": tools},
			); err != nil {
				return fmt.Errorf("bulk_grant: record audit: %w", err)
			}
		}
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
		Tool        string  `json:"tool"`
		Pattern     *string `json:"pattern"`
		AutoGranted bool    `json:"autoGranted"`
		RequestID   *string `json:"requestId,omitempty"`
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
		h.broadcastEnrichedEvent(r.Context(), "permission_request", sr.TaskID)
	}

	return jsonReply(w, http.StatusOK, results)
}

// bulkResolvePermissionRequests handles POST /api/permission-requests/bulk-resolve.
func (h *Handler) bulkResolvePermissionRequests(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		TaskID        string   `json:"taskId"`
		Outcome       string   `json:"outcome"`
		PermissionIDs []string `json:"permissionIds"`
		All           bool     `json:"all"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if body.TaskID == "" {
		return apierr.NewAppError(http.StatusBadRequest, "taskId is required")
	}
	if body.Outcome != "granted" && body.Outcome != "denied" {
		return apierr.NewAppError(http.StatusBadRequest, "outcome must be granted or denied")
	}

	outcome := body.Outcome

	// Object-level authz: only the task's own pending requests are resolvable,
	// so a caller cannot flip permission requests belonging to a different task.
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

	var idsToResolve []string
	resolveErrors := []string{}
	if body.All {
		for _, req := range pending {
			idsToResolve = append(idsToResolve, req.ID)
		}
	} else {
		allowed := make(map[string]bool, len(pending))
		for _, req := range pending {
			allowed[req.ID] = true
		}
		for _, id := range body.PermissionIDs {
			if allowed[id] {
				idsToResolve = append(idsToResolve, id)
			} else {
				resolveErrors = append(resolveErrors, fmt.Sprintf("permission %s not pending for task %s", id, body.TaskID))
			}
		}
	}

	resolvedCount := 0
	for _, id := range idsToResolve {
		if err := h.permRepo.ResolvePermissionRequest(r.Context(), id, outcome); err != nil {
			resolveErrors = append(resolveErrors, err.Error())
			continue
		}
		resolvedCount++
	}

	// When granted, create task_permissions from the resolved requests so the
	// respawned agent's allow-list includes the newly approved tools, then resume.
	if outcome == "granted" && len(idsToResolve) > 0 {
		resolveSet := make(map[string]bool, len(idsToResolve))
		for _, id := range idsToResolve {
			resolveSet[id] = true
		}
		var entries []repo.GrantEntry
		for _, req := range pending {
			if !resolveSet[req.ID] {
				continue
			}
			entries = append(entries, repo.GrantEntry{Tool: req.Tool, Pattern: req.Pattern})
		}
		if _, errs := h.grantValidatedEntries(r.Context(), body.TaskID, entries); len(errs) > 0 {
			resolveErrors = append(resolveErrors, errs...)
		}
		if _, err := h.orchestrator.ResumeFromUser(r.Context(), body.TaskID); err != nil {
			slog.Warn("bulk_resolve: ResumeFromUser failed", "taskID", body.TaskID, "err", err)
		}
	}

	h.broadcastEnrichedUpdate(r.Context(), body.TaskID)
	return jsonReply(w, http.StatusOK, map[string]any{
		"resolved": resolvedCount,
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

// grantValidatedEntries validates each entry as a human override and persists
// the safe ones via BulkGrantPermissions. Called exclusively from human REST
// resolve endpoints, so override=true applies: the Bash allow-list and
// injection guard are bypassed but a non-empty pattern is still required, and
// WebFetch still needs a domain. Invalid entries are collected and returned as
// error strings rather than aborting the call.
func (h *Handler) grantValidatedEntries(ctx context.Context, taskID string, entries []repo.GrantEntry) ([]*ent.TaskPermission, []string) {
	var safe []repo.GrantEntry
	var errs []string
	for _, e := range entries {
		pattern := ""
		if e.Pattern != nil {
			pattern = *e.Pattern
		}
		if err := permissions.ValidateGrantEntryWithOverride(e.Tool, pattern, true); err != nil {
			errs = append(errs, fmt.Sprintf("grant skipped (%s %s): %v", e.Tool, pattern, err))
			continue
		}
		e.ManualOverride = true
		safe = append(safe, e)
	}
	if len(safe) == 0 {
		return nil, errs
	}
	granted, err := h.permRepo.BulkGrantPermissions(ctx, taskID, safe)
	if err != nil {
		errs = append(errs, fmt.Sprintf("BulkGrantPermissions: %v", err))
		return nil, errs
	}
	tools := make([]string, 0, len(granted))
	for _, p := range granted {
		tools = append(tools, p.Tool)
	}
	slog.Info("permission grant from resolved request", "taskID", taskID, "tools", tools)
	return granted, errs
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
