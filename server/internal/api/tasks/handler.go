package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
	"github.com/lx-wnk/agent-dashboard/server/internal/validation"
)

const (
	maxTitleChars       = 200
	maxDescriptionChars = 10_000
)

func jsonReply(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// OrchestratorIface is the subset of PipelineOrchestrator consumed by the handler.
type OrchestratorIface interface {
	ProgressTask(ctx context.Context, taskID string, opts *pipeline.ProgressOpts) (*ent.StageRun, error)
	ResumeFromUser(ctx context.Context, taskID string) (*ent.StageRun, error)
	NotifyTaskTerminated(ctx context.Context, taskID, stage string)
	InvalidateConfigCache()
}

// Handler handles task REST endpoints.
type Handler struct {
	taskRepo     repo.TaskRepo
	srRepo       repo.StageRunRepo
	permRepo     repo.PermissionRepo
	auditRepo    repo.AuditRepo
	cfgRepo      repo.PipelineConfigRepo
	depRepo      repo.DependencyRepo
	orchestrator OrchestratorIface
	broadcaster  *sse.TaskBroadcaster
}

// Deps groups all constructor dependencies.
type Deps struct {
	TaskRepo     repo.TaskRepo
	SRRepo       repo.StageRunRepo
	PermRepo     repo.PermissionRepo
	AuditRepo    repo.AuditRepo
	CfgRepo      repo.PipelineConfigRepo
	DepRepo      repo.DependencyRepo
	Orchestrator OrchestratorIface
	Broadcaster  *sse.TaskBroadcaster
}

func NewHandler(deps Deps) *Handler {
	return &Handler{
		taskRepo:     deps.TaskRepo,
		srRepo:       deps.SRRepo,
		permRepo:     deps.PermRepo,
		auditRepo:    deps.AuditRepo,
		cfgRepo:      deps.CfgRepo,
		depRepo:      deps.DepRepo,
		orchestrator: deps.Orchestrator,
		broadcaster:  deps.Broadcaster,
	}
}

// Mount registers all task routes on the given chi.Router.
func (h *Handler) Mount(r chi.Router) {
	// Existing task routes.
	r.Get("/api/tasks", apierr.ErrorMiddleware(h.list))
	r.Get("/api/tasks/stream", h.stream)
	r.Get("/api/tasks/{id}", apierr.ErrorMiddleware(h.getOne))
	r.Get("/api/tasks/{id}/stage-runs", apierr.ErrorMiddleware(h.listStageRuns))
	r.Get("/api/tasks/{id}/audit", apierr.ErrorMiddleware(h.listAudit))
	r.Get("/api/tasks/{id}/permissions", apierr.ErrorMiddleware(h.listPermissions))
	r.Post("/api/tasks", apierr.ErrorMiddleware(h.create))
	r.Patch("/api/tasks/{id}", apierr.ErrorMiddleware(h.update))
	r.Delete("/api/tasks/{id}", apierr.ErrorMiddleware(h.delete))
	r.Post("/api/tasks/{id}/progress", apierr.ErrorMiddleware(h.progress))
	r.Post("/api/tasks/{id}/cancel", apierr.ErrorMiddleware(h.cancel))
	r.Post("/api/tasks/{id}/retry", apierr.ErrorMiddleware(h.retry))
	r.Post("/api/tasks/{id}/permissions", apierr.ErrorMiddleware(h.grantPermission))
	r.Delete("/api/tasks/{id}/permissions/{permID}", apierr.ErrorMiddleware(h.revokePermission))
	r.Post("/api/tasks/{id}/permission-requests/{reqID}/resolve", apierr.ErrorMiddleware(h.resolvePermissionRequest))

	// Pipeline config.
	r.Get("/api/pipeline/config", apierr.ErrorMiddleware(h.getPipelineConfig))
	r.Put("/api/pipeline/config", apierr.ErrorMiddleware(h.putPipelineConfig))
	r.Get("/api/pipeline/recommendation", apierr.ErrorMiddleware(h.getPipelineRecommendation))

	// Cost breakdown and stage output.
	r.Get("/api/tasks/{id}/cost-breakdown", apierr.ErrorMiddleware(h.getCostBreakdown))
	r.Get("/api/tasks/{id}/stage-runs/{runId}/agent-output", apierr.ErrorMiddleware(h.getStageRunAgentOutput))

	// Permission requests.
	r.Get("/api/tasks/{id}/permission-requests", apierr.ErrorMiddleware(h.listPermissionRequests))
	r.Post("/api/tasks/{id}/permissions/bulk", apierr.ErrorMiddleware(h.bulkGrantPermissions))
	r.Post("/api/permission-requests", apierr.ErrorMiddleware(h.createPermissionRequest))
	r.Post("/api/permission-requests/bulk", apierr.ErrorMiddleware(h.bulkCreatePermissionRequests))
	r.Post("/api/permission-requests/bulk-resolve", apierr.ErrorMiddleware(h.bulkResolvePermissionRequests))

	// Dependencies.
	r.Get("/api/tasks/{id}/dependencies", apierr.ErrorMiddleware(h.listDependencies))
	r.Get("/api/tasks/{id}/dependents", apierr.ErrorMiddleware(h.listDependents))
	r.Post("/api/tasks/{id}/dependencies", apierr.ErrorMiddleware(h.addDependency))
	r.Delete("/api/tasks/{id}/dependencies/{depId}", apierr.ErrorMiddleware(h.removeDependency))

	// Resume stage.
	r.Post("/api/tasks/{id}/resume-stage", apierr.ErrorMiddleware(h.resumeStage))

	// Analysis agent spawn.
	r.Post("/api/tasks/{id}/analyze", apierr.ErrorMiddleware(h.analyzeTask))

	// Git status + actions.
	r.Get("/api/tasks/{id}/git-status", apierr.ErrorMiddleware(h.getGitStatusHandler))
	r.Post("/api/tasks/{id}/git-action", apierr.ErrorMiddleware(h.gitActionHandler))
	r.Post("/api/tasks/{id}/run", apierr.ErrorMiddleware(h.taskRunHandler))

	// Notification preferences + config.
	r.Get("/api/notifications/preferences", apierr.ErrorMiddleware(h.listNotificationPreferences))
	r.Put("/api/notifications/preferences/{eventType}", apierr.ErrorMiddleware(h.putNotificationPreference))
	r.Get("/api/notifications/config", apierr.ErrorMiddleware(h.getNotificationConfig))
	r.Put("/api/notifications/config", apierr.ErrorMiddleware(h.putNotificationConfig))

	// Global audit + webhook HMAC settings.
	r.Get("/api/audit", apierr.ErrorMiddleware(h.listGlobalAudit))
	r.Get("/api/settings/webhook-hmac", apierr.ErrorMiddleware(h.getWebhookHMAC))
	r.Post("/api/settings/webhook-hmac", apierr.ErrorMiddleware(h.putWebhookHMAC))

	// Export + feedback.
	r.Get("/api/tasks/export", apierr.ErrorMiddleware(h.exportTasks))
	r.Get("/api/tasks/{id}/feedback", apierr.ErrorMiddleware(h.listFeedback))
}

func (h *Handler) broadcastEnrichedUpdate(ctx context.Context, taskID string) {
	t, err := h.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return
	}
	enriched, err := EnrichTask(ctx, t, h.srRepo, h.permRepo)
	if err != nil {
		return
	}
	h.broadcaster.Broadcast(sse.TaskEvent{Type: "task_updated", TaskID: taskID, Payload: enriched})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	payload, _ := auth.PayloadFromContext(r.Context())
	tasks, err := h.taskRepo.ListForUser(r.Context(), payload.Sub, payload.IsAdmin)
	if err != nil {
		return fmt.Errorf("tasks.list: %w", err)
	}
	stage := r.URL.Query().Get("stage")
	if stage != "" {
		var filtered []*ent.Task
		for _, t := range tasks {
			if t.CurrentStage == stage {
				filtered = append(filtered, t)
			}
		}
		tasks = filtered
	}
	enriched, err := EnrichTasksBulk(r.Context(), tasks, h.srRepo, h.permRepo)
	if err != nil {
		return fmt.Errorf("tasks.list.enrich: %w", err)
	}
	return jsonReply(w, http.StatusOK, enriched)
}

func (h *Handler) getOne(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	t, err := h.taskRepo.GetByID(r.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			return apierr.ErrNotFound
		}
		return fmt.Errorf("tasks.getOne: %w", err)
	}
	enriched, err := EnrichTask(r.Context(), t, h.srRepo, h.permRepo)
	if err != nil {
		return fmt.Errorf("tasks.getOne.enrich: %w", err)
	}
	return jsonReply(w, http.StatusOK, enriched)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Slug          string  `json:"slug"`
		Title         string  `json:"title"`
		Description   *string `json:"description"`
		Cwd           string  `json:"cwd"`
		Priority      string  `json:"priority"`
		Stage         string  `json:"stage"`
		SilverBullet  bool    `json:"silverBullet"`
		MaxIterations int     `json:"maxIterations"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if !validation.IsValidSlug(body.Slug) {
		return apierr.NewAppError(http.StatusBadRequest, validation.SlugPatternMessage)
	}
	if body.Title == "" || len(body.Title) > maxTitleChars {
		return apierr.NewAppError(http.StatusBadRequest, "title is required and must be <= 200 characters")
	}
	if body.Cwd == "" {
		return apierr.NewAppError(http.StatusBadRequest, "cwd is required")
	}
	if _, err := h.taskRepo.GetBySlug(r.Context(), body.Slug); err == nil {
		return apierr.NewAppError(http.StatusConflict, "slug already exists")
	}
	priority := body.Priority
	if priority == "" {
		priority = "medium"
	}
	stage := body.Stage
	if stage == "" {
		stage = "concept"
	}
	maxIter := body.MaxIterations
	if maxIter <= 0 {
		maxIter = 20
	}
	payload, _ := auth.PayloadFromContext(r.Context())
	userID := payload.Sub
	task, err := h.taskRepo.Create(r.Context(), repo.CreateTaskInput{
		Slug:                body.Slug,
		Title:               body.Title,
		Description:         body.Description,
		Cwd:                 body.Cwd,
		UserID:              &userID,
		Priority:            priority,
		CurrentStage:        stage,
		SilverBullet:        body.SilverBullet,
		MaxIterations:       maxIter,
		StageTimeoutSeconds: 1800,
	})
	if err != nil {
		return fmt.Errorf("tasks.create: %w", err)
	}
	enriched, _ := EnrichTask(r.Context(), task, h.srRepo, h.permRepo)
	h.broadcaster.Broadcast(sse.TaskEvent{Type: "task_created", TaskID: task.ID, Payload: enriched})
	return jsonReply(w, http.StatusCreated, enriched)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := h.taskRepo.GetByID(r.Context(), id); err != nil {
		return apierr.ErrNotFound
	}
	var body struct {
		Title         *string `json:"title"`
		Description   *string `json:"description"`
		Priority      *string `json:"priority"`
		SilverBullet  *bool   `json:"silverBullet"`
		MaxIterations *int    `json:"maxIterations"`
		CurrentStage  *string `json:"currentStage"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if body.CurrentStage != nil {
		return apierr.NewAppError(http.StatusBadRequest, "currentStage cannot be set via PATCH — use /progress, /cancel, or /retry")
	}
	updated, err := h.taskRepo.Update(r.Context(), id, repo.UpdateTaskInput{
		Title:         body.Title,
		Description:   body.Description,
		Priority:      body.Priority,
		SilverBullet:  body.SilverBullet,
		MaxIterations: body.MaxIterations,
	})
	if err != nil {
		return fmt.Errorf("tasks.update: %w", err)
	}
	h.broadcastEnrichedUpdate(r.Context(), id)
	return jsonReply(w, http.StatusOK, updated)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if err := h.taskRepo.Delete(r.Context(), id); err != nil {
		if ent.IsNotFound(err) {
			return apierr.ErrNotFound
		}
		return fmt.Errorf("tasks.delete: %w", err)
	}
	h.broadcaster.Broadcast(sse.TaskEvent{Type: "task_deleted", TaskID: id})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) progress(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	sr, err := h.orchestrator.ProgressTask(r.Context(), id, nil)
	if err != nil {
		return fmt.Errorf("tasks.progress: %w", err)
	}
	if sr == nil {
		return apierr.NewAppError(http.StatusConflict, "task cannot progress (terminal, missing, or slot full)")
	}
	task, _ := h.taskRepo.GetByID(r.Context(), id)
	h.broadcastEnrichedUpdate(r.Context(), id)
	return jsonReply(w, http.StatusOK, map[string]any{"task": task, "stageRun": sr})
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	t, err := h.taskRepo.GetByID(r.Context(), id)
	if err != nil {
		return apierr.ErrNotFound
	}
	if t.CurrentStage == "cancelled" || t.CurrentStage == "done" {
		return apierr.NewAppError(http.StatusBadRequest, "task is already "+t.CurrentStage)
	}
	cancelled := "cancelled"
	updated, err := h.taskRepo.Update(r.Context(), id, repo.UpdateTaskInput{CurrentStage: &cancelled})
	if err != nil {
		return fmt.Errorf("tasks.cancel: %w", err)
	}
	h.orchestrator.NotifyTaskTerminated(r.Context(), id, "cancelled")
	h.broadcastEnrichedUpdate(r.Context(), id)
	return jsonReply(w, http.StatusOK, updated)
}

func (h *Handler) retry(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	t, err := h.taskRepo.GetByID(r.Context(), id)
	if err != nil {
		return apierr.ErrNotFound
	}
	latest, err := h.srRepo.GetLatestByTaskAndStage(r.Context(), id, t.CurrentStage)
	if err != nil || latest == nil || latest.Status != "failed" {
		return apierr.NewAppError(http.StatusConflict, "task has no failed stage run to retry on its current stage")
	}
	var body struct {
		AdditionalPrompt string `json:"additionalPrompt"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	_ = h.auditRepo.Append(r.Context(), repo.AppendAuditInput{
		TaskID: id, Actor: "user", Action: "retry_requested",
		Details: map[string]any{"stage": latest.Stage, "iteration": latest.Iteration},
	})
	var opts *pipeline.ProgressOpts
	if body.AdditionalPrompt != "" {
		opts = &pipeline.ProgressOpts{UserAdditionalPrompt: body.AdditionalPrompt}
	}
	sr, err := h.orchestrator.ProgressTask(r.Context(), id, opts)
	if err != nil {
		return fmt.Errorf("tasks.retry: %w", err)
	}
	if sr == nil {
		return apierr.NewAppError(http.StatusConflict, "task could not progress")
	}
	h.broadcastEnrichedUpdate(r.Context(), id)
	return jsonReply(w, http.StatusOK, sr)
}

func (h *Handler) listStageRuns(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	runs, err := h.srRepo.ListForTask(r.Context(), id)
	if err != nil {
		return fmt.Errorf("tasks.listStageRuns: %w", err)
	}
	return jsonReply(w, http.StatusOK, runs)
}

func (h *Handler) listAudit(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	logs, err := h.auditRepo.ListForTask(r.Context(), id)
	if err != nil {
		return fmt.Errorf("tasks.listAudit: %w", err)
	}
	return jsonReply(w, http.StatusOK, logs)
}

func (h *Handler) listPermissions(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	perms, err := h.permRepo.ListTaskPermissions(r.Context(), id)
	if err != nil {
		return fmt.Errorf("tasks.listPermissions: %w", err)
	}
	return jsonReply(w, http.StatusOK, perms)
}

func (h *Handler) grantPermission(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	var body struct {
		Tool    string  `json:"tool"`
		Pattern *string `json:"pattern"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if body.Tool == "" {
		return apierr.NewAppError(http.StatusBadRequest, "tool is required")
	}
	perm, err := h.permRepo.CreateTaskPermission(r.Context(), repo.CreateTaskPermissionInput{
		TaskID:  id,
		Tool:    body.Tool,
		Pattern: body.Pattern,
		Granted: true,
	})
	if err != nil {
		return fmt.Errorf("tasks.grantPermission: %w", err)
	}
	return jsonReply(w, http.StatusCreated, perm)
}

func (h *Handler) revokePermission(w http.ResponseWriter, r *http.Request) error {
	permID := chi.URLParam(r, "permID")
	if err := h.permRepo.DeleteTaskPermission(r.Context(), permID); err != nil {
		return fmt.Errorf("tasks.revokePermission: %w", err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) resolvePermissionRequest(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	reqID := chi.URLParam(r, "reqID")
	var body struct {
		Outcome string `json:"outcome"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if body.Outcome != "granted" && body.Outcome != "denied" {
		return apierr.NewAppError(http.StatusBadRequest, "outcome must be granted or denied")
	}
	if err := h.permRepo.ResolvePermissionRequest(r.Context(), reqID, body.Outcome); err != nil {
		return fmt.Errorf("tasks.resolvePermissionRequest: %w", err)
	}
	resolved, err := h.permRepo.GetPermissionRequest(r.Context(), reqID)
	if err != nil {
		return fmt.Errorf("tasks.resolvePermissionRequest.get: %w", err)
	}
	if body.Outcome == "granted" {
		if _, err := h.orchestrator.ResumeFromUser(r.Context(), id); err != nil {
			slog.Warn("resolvePermissionRequest: ResumeFromUser failed", "taskID", id, "err", err)
		}
	}
	h.broadcastEnrichedUpdate(r.Context(), id)
	return jsonReply(w, http.StatusOK, resolved)
}

func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	sub := h.broadcaster.Subscribe()
	defer h.broadcaster.Unsubscribe(sub)

	for {
		select {
		case data, ok := <-sub:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
