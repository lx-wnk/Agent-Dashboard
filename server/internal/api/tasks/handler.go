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
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/rawrepo"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
	"github.com/lx-wnk/agent-dashboard/server/internal/taskcontrol"
	"github.com/lx-wnk/agent-dashboard/server/internal/validation"
	"github.com/lx-wnk/agent-dashboard/server/internal/worktree"
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

// RefineStatusReader exposes refinement run status to task enrichment without
// a compile-time dependency on the refine runner. Implemented by *refine.Runner.
type RefineStatusReader interface {
	State(taskID string) (status, errMsg string)
}

// OrchestratorIface is the subset of PipelineOrchestrator consumed by the handler.
type OrchestratorIface interface {
	ProgressTask(ctx context.Context, taskID string, opts *pipeline.ProgressOpts) (*ent.StageRun, error)
	ResumeFromUser(ctx context.Context, taskID, userPrompt string) (*ent.StageRun, error)
	RequeueForUser(ctx context.Context, taskID, userPrompt string) (*ent.StageRun, error)
	NotifyTaskTerminated(ctx context.Context, taskID, stage string)
	InvalidateConfigCache()
	ClearStalePendingPermissions(ctx context.Context, taskID string)
}

// Handler handles task REST endpoints.
type Handler struct {
	client            *ent.Client
	taskRepo          repo.TaskRepo
	srRepo            repo.StageRunRepo
	srBulkRepo        rawrepo.StageRunBulkRepo
	permRepo          repo.PermissionRepo
	presetRepo        repo.PermissionPresetRepo
	auditRepo         repo.AuditEventRepo
	auditEventRepo    repo.AuditEventRepo
	cfgRepo           repo.PipelineConfigRepo
	depRepo           repo.DependencyRepo
	projectRepo       repo.ProjectRepo
	projectFolderRepo repo.ProjectFolderRepo
	spawnerRepo       repo.SpawnerRepo
	orchestrator      OrchestratorIface
	broadcaster       *sse.TaskBroadcaster
	worktreeMgr       WorktreeStatusProvider
	refineReader      RefineStatusReader
}

// Deps groups all constructor dependencies.
type Deps struct {
	// Client is the ent client used to open transactions for atomic multi-write operations.
	// When nil, transactional paths fall back to individual writes (e.g. in tests).
	Client   *ent.Client
	TaskRepo repo.TaskRepo
	SRRepo   repo.StageRunRepo
	// SRBulkRepo is the window-function bulk repo used by EnrichTasksBulk to
	// fetch the exact latest stage_run per task regardless of iteration count.
	SRBulkRepo        rawrepo.StageRunBulkRepo
	PermRepo          repo.PermissionRepo
	PresetRepo        repo.PermissionPresetRepo
	AuditRepo         repo.AuditEventRepo
	AuditEventRepo    repo.AuditEventRepo
	CfgRepo           repo.PipelineConfigRepo
	DepRepo           repo.DependencyRepo
	ProjectRepo       repo.ProjectRepo
	ProjectFolderRepo repo.ProjectFolderRepo
	SpawnerRepo       repo.SpawnerRepo
	Orchestrator      OrchestratorIface
	Broadcaster       *sse.TaskBroadcaster
	WorktreeMgr       WorktreeStatusProvider
	RefineReader      RefineStatusReader
}

func NewHandler(deps Deps) *Handler {
	return &Handler{
		client:            deps.Client,
		taskRepo:          deps.TaskRepo,
		srRepo:            deps.SRRepo,
		srBulkRepo:        deps.SRBulkRepo,
		permRepo:          deps.PermRepo,
		presetRepo:        deps.PresetRepo,
		auditRepo:         deps.AuditRepo,
		auditEventRepo:    deps.AuditEventRepo,
		cfgRepo:           deps.CfgRepo,
		depRepo:           deps.DepRepo,
		projectRepo:       deps.ProjectRepo,
		projectFolderRepo: deps.ProjectFolderRepo,
		spawnerRepo:       deps.SpawnerRepo,
		orchestrator:      deps.Orchestrator,
		broadcaster:       deps.Broadcaster,
		worktreeMgr:       deps.WorktreeMgr,
		refineReader:      deps.RefineReader,
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
	r.Post("/api/tasks/{id}/rank", apierr.ErrorMiddleware(h.rankTask))
	r.Delete("/api/tasks/{id}", apierr.ErrorMiddleware(h.delete))
	r.Post("/api/tasks/{id}/advance", apierr.ErrorMiddleware(h.advance))
	// NOTE: advance is the preferred entry point; progress is kept for compatibility.
	r.Post("/api/tasks/{id}/progress", apierr.ErrorMiddleware(h.progress))
	r.Post("/api/tasks/{id}/cancel", apierr.ErrorMiddleware(h.cancel))
	r.Post("/api/tasks/{id}/retry", apierr.ErrorMiddleware(h.retry))
	r.Post("/api/tasks/{id}/hold", apierr.ErrorMiddleware(h.hold))
	r.Post("/api/tasks/{id}/resume", apierr.ErrorMiddleware(h.resume))
	r.Post("/api/tasks/{id}/approve-all-pending", apierr.ErrorMiddleware(h.approveAllPending))
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

	// Worktree status (branch, ahead/behind vs origin/<base>, dirty flag).
	r.Get("/api/tasks/{id}/worktree", apierr.ErrorMiddleware(h.getWorktreeStatusHandler))
	r.Post("/api/tasks/{id}/worktree", apierr.ErrorMiddleware(h.createWorktreeHandler))
	r.Delete("/api/tasks/{id}/worktree", apierr.ErrorMiddleware(h.removeWorktreeHandler))

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

// MountAgentIngress registers agent-initiated permission-request CREATE routes.
// Called server-to-server by the channel bridge with a bearer MCP token (no
// Origin/JWT), so the caller mounts them OUTSIDE the JWT/same-origin group,
// behind McpAuthMiddleware. Resolution/grant routes stay in Mount.
func (h *Handler) MountAgentIngress(r chi.Router) {
	r.Post("/api/permission-requests", apierr.ErrorMiddleware(h.createPermissionRequest))
	r.Post("/api/permission-requests/bulk", apierr.ErrorMiddleware(h.bulkCreatePermissionRequests))
}

func (h *Handler) broadcastEnrichedUpdate(ctx context.Context, taskID string) {
	h.broadcastEnrichedEvent(ctx, "task_updated", taskID)
}

// BroadcastTaskUpdate is the runner's onRunChange callback target.
func (h *Handler) BroadcastTaskUpdate(taskID string) {
	h.broadcastEnrichedUpdate(context.Background(), taskID)
}

// applyRefineStatus fills RefineStatus/RefineError from the injected reader and
// recomputes AvailableActions so the concept-stage refining case is reflected.
func (h *Handler) applyRefineStatus(e *EnrichedTask, taskID string) {
	if h.refineReader == nil || e == nil {
		return
	}
	status, errMsg := h.refineReader.State(taskID)
	e.RefineStatus = &status
	if errMsg != "" {
		e.RefineError = &errMsg
	}
	// Refine status can change which action is primary for concept-stage tasks.
	e.RecomputeAvailableActions()
}

// broadcastEnrichedEvent fetches and enriches the task, then broadcasts it with the given event type.
// Marshalling or DB errors are silently dropped — the 60-second polling fallback will catch any missed update.
func (h *Handler) broadcastEnrichedEvent(ctx context.Context, eventType string, taskID string) {
	t, err := h.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return
	}
	enriched, err := EnrichTask(ctx, t, h.srRepo, h.permRepo)
	if err != nil {
		return
	}
	h.applyRefineStatus(enriched, taskID)
	h.broadcaster.Broadcast(sse.TaskEvent{Type: eventType, TaskID: taskID, Payload: enriched})
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
	enriched, err := EnrichTasksBulk(r.Context(), tasks, h.srRepo, h.permRepo, h.srBulkRepo)
	if err != nil {
		return fmt.Errorf("tasks.list.enrich: %w", err)
	}
	for _, e := range enriched {
		h.applyRefineStatus(e, e.ID)
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
	h.applyRefineStatus(enriched, id)
	return jsonReply(w, http.StatusOK, enriched)
}

// clampNegativeBudget collapses a negative budget to 0 (disabled). 0 already
// means "no budget"; a negative value is nonsensical and would be silently
// treated as disabled by the enforcement guard anyway.
func clampNegativeBudget(p *int) {
	if p != nil && *p < 0 {
		*p = 0
	}
}

// CreateTaskParams is the resolved input for creating a pipeline task, shared by
// the HTTP create handler and the scheduler materializer. Defaults for priority,
// stage, maxIterations, stageTimeoutSeconds, and budgets are applied inside
// CreateTaskFromInput when their zero value is passed.
type CreateTaskParams struct {
	Slug            string
	Title           string
	Description     *string
	Cwd             string
	Priority        string
	Stage           string
	SilverBullet    bool
	MaxIterations   int
	SourceBranch    *string
	TargetBranch    *string
	TokenBudget     *int
	CostBudgetCents *int
	ProjectID       string // empty = unset
	SpawnerID       string // empty = unset
	UserID          *string
	Metadata        map[string]any
	Autonomy        *string
}

// CreateTaskFromInput is the reusable task-creation core: it checks slug
// uniqueness, resolves and validates the optional project/spawner references,
// applies defaults, persists the task, and broadcasts task_created. It returns
// the enriched task. Errors are *apierr.AppError where they map to a client
// status; the HTTP handler returns them directly and non-HTTP callers (the
// scheduler) treat any error as a skip. Body-format validation (slug pattern,
// title length, required cwd) stays with the HTTP handler.
func (h *Handler) CreateTaskFromInput(ctx context.Context, p CreateTaskParams) (*EnrichedTask, error) {
	if _, err := h.taskRepo.GetBySlug(ctx, p.Slug); err == nil {
		return nil, apierr.NewAppError(http.StatusConflict, "slug already exists")
	}
	if p.SourceBranch != nil && *p.SourceBranch != "" {
		n, err := h.taskRepo.CountActiveBySourceBranch(ctx, *p.SourceBranch, "")
		if err != nil {
			return nil, fmt.Errorf("tasks.create.sourceBranchCheck: %w", err)
		}
		if n > 0 {
			return nil, apierr.NewAppError(http.StatusConflict, "source_branch already in use by another active task")
		}
		// Authoritative git check: a leftover/manual worktree may hold the branch
		// even when no active task references it.
		if held, gErr := worktree.BranchCheckedOutAt(ctx, p.Cwd, *p.SourceBranch); gErr == nil && held != "" {
			return nil, apierr.NewAppError(http.StatusConflict,
				fmt.Sprintf("source_branch %q already checked out at %s", *p.SourceBranch, held))
		}
	}

	// Resolve optional project + spawner. Empty string = unset.
	var projectIDPtr, spawnerIDPtr *string
	if p.ProjectID != "" {
		if h.projectRepo == nil {
			return nil, apierr.NewAppError(http.StatusInternalServerError, "project repo not configured")
		}
		if _, err := h.projectRepo.GetByID(ctx, p.ProjectID); err != nil {
			if ent.IsNotFound(err) {
				return nil, apierr.NewAppError(http.StatusNotFound, "project not found")
			}
			return nil, fmt.Errorf("tasks.create.projectLookup: %w", err)
		}
		pid := p.ProjectID
		projectIDPtr = &pid
	}
	if p.SpawnerID != "" {
		if h.spawnerRepo == nil {
			return nil, apierr.NewAppError(http.StatusInternalServerError, "spawner repo not configured")
		}
		if _, err := h.spawnerRepo.GetByID(ctx, p.SpawnerID); err != nil {
			if ent.IsNotFound(err) {
				return nil, apierr.NewAppError(http.StatusNotFound, "spawner not found")
			}
			return nil, fmt.Errorf("tasks.create.spawnerLookup: %w", err)
		}
		sid := p.SpawnerID
		spawnerIDPtr = &sid
	}

	priority := p.Priority
	if priority == "" {
		priority = db.DefaultPriority
	}
	stage := p.Stage
	if stage == "" {
		stage = db.DefaultStage
	}
	maxIter := p.MaxIterations
	if maxIter <= 0 {
		maxIter = db.DefaultMaxIterations
	}
	costBudget := p.CostBudgetCents
	if costBudget == nil {
		v := db.DefaultCostBudgetCents
		costBudget = &v
	}
	clampNegativeBudget(costBudget)
	tokenBudget := p.TokenBudget
	if tokenBudget == nil {
		v := db.DefaultTokenBudget
		tokenBudget = &v
	}
	clampNegativeBudget(tokenBudget)
	task, err := h.taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug:                p.Slug,
		Title:               p.Title,
		Description:         p.Description,
		Cwd:                 p.Cwd,
		UserID:              p.UserID,
		Priority:            priority,
		CurrentStage:        stage,
		SilverBullet:        p.SilverBullet,
		MaxIterations:       maxIter,
		StageTimeoutSeconds: db.DefaultStageTimeoutSeconds,
		SourceBranch:        p.SourceBranch,
		TargetBranch:        p.TargetBranch,
		TokenBudget:         tokenBudget,
		CostBudgetCents:     costBudget,
		ProjectID:           projectIDPtr,
		SpawnerID:           spawnerIDPtr,
		Autonomy:            p.Autonomy,
		Metadata:            p.Metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("tasks.create: %w", err)
	}
	enriched, _ := EnrichTask(ctx, task, h.srRepo, h.permRepo)
	h.applyRefineStatus(enriched, task.ID)
	h.broadcaster.Broadcast(sse.TaskEvent{Type: "task_created", TaskID: task.ID, Payload: enriched})
	return enriched, nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Slug            string  `json:"slug"`
		Title           string  `json:"title"`
		Description     *string `json:"description"`
		Cwd             string  `json:"cwd"`
		Priority        string  `json:"priority"`
		Stage           string  `json:"stage"`
		SilverBullet    bool    `json:"silverBullet"`
		MaxIterations   int     `json:"maxIterations"`
		CostBudgetCents *int    `json:"costBudgetCents"`
		TokenBudget     *int    `json:"tokenBudget"`
		ProjectID       string  `json:"projectId"`
		SpawnerID       string  `json:"spawnerId"`
		Autonomy        *string `json:"autonomy"`
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
	if body.Description != nil && len(*body.Description) > maxDescriptionChars {
		return apierr.NewAppError(http.StatusBadRequest, "description must be <= 10000 characters")
	}
	if body.Cwd == "" {
		return apierr.NewAppError(http.StatusBadRequest, "cwd is required")
	}
	if body.Autonomy != nil {
		if _, ok := taskcontrol.ValidAutonomyValues[*body.Autonomy]; !ok {
			return apierr.NewAppError(http.StatusBadRequest, "invalid autonomy")
		}
	}

	payload, _ := auth.PayloadFromContext(r.Context())
	userID := payload.Sub
	enriched, err := h.CreateTaskFromInput(r.Context(), CreateTaskParams{
		Slug:            body.Slug,
		Title:           body.Title,
		Description:     body.Description,
		Cwd:             body.Cwd,
		Priority:        body.Priority,
		Stage:           body.Stage,
		SilverBullet:    body.SilverBullet,
		MaxIterations:   body.MaxIterations,
		CostBudgetCents: body.CostBudgetCents,
		TokenBudget:     body.TokenBudget,
		ProjectID:       body.ProjectID,
		SpawnerID:       body.SpawnerID,
		UserID:          &userID,
		Autonomy:        body.Autonomy,
	})
	if err != nil {
		return err
	}

	// Non-blocking cwd_not_in_project warning when project is set and cwd does
	// not match any of its folder paths exactly. Response shape stays flat for
	// the no-warning path (backwards-compat); a wrapper is used only when the
	// warning fires.
	if body.ProjectID != "" && h.projectFolderRepo != nil {
		if warn := h.cwdNotInProjectWarning(r.Context(), body.ProjectID, body.Cwd); warn {
			return jsonReply(w, http.StatusCreated, map[string]any{
				"task":    enriched,
				"warning": "cwd_not_in_project",
			})
		}
	}
	return jsonReply(w, http.StatusCreated, enriched)
}

// cwdNotInProjectWarning returns true when the given cwd does not exactly match
// any folder.path of the given project. A folder-list lookup failure is treated
// as "no warning" so the create/update path never blocks on this check.
func (h *Handler) cwdNotInProjectWarning(ctx context.Context, projectID, cwd string) bool {
	if cwd == "" || h.projectFolderRepo == nil {
		return false
	}
	folders, err := h.projectFolderRepo.ListByProject(ctx, projectID)
	if err != nil || len(folders) == 0 {
		return false
	}
	for _, f := range folders {
		if f.Path == cwd {
			return false
		}
	}
	return true
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	existing, err := h.taskRepo.GetByID(r.Context(), id)
	if err != nil {
		return apierr.ErrNotFound
	}
	var body struct {
		Title           *string         `json:"title"`
		Description     *string         `json:"description"`
		Priority        *string         `json:"priority"`
		SilverBullet    *bool           `json:"silverBullet"`
		MaxIterations   *int            `json:"maxIterations"`
		CostBudgetCents *int            `json:"costBudgetCents"`
		TokenBudget     *int            `json:"tokenBudget"`
		CurrentStage    *string         `json:"currentStage"`
		Cwd             *string         `json:"cwd"`
		ProjectID       json.RawMessage `json:"projectId"`
		SpawnerID       json.RawMessage `json:"spawnerId"`
		Autonomy        *string         `json:"autonomy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if body.CurrentStage != nil {
		return apierr.NewAppError(http.StatusBadRequest, "currentStage cannot be set via PATCH — use /progress, /cancel, or /retry")
	}
	if body.Description != nil && len(*body.Description) > maxDescriptionChars {
		return apierr.NewAppError(http.StatusBadRequest, "description must be <= 10000 characters")
	}
	if body.Autonomy != nil {
		if _, ok := taskcontrol.ValidAutonomyValues[*body.Autonomy]; !ok {
			return apierr.NewAppError(http.StatusBadRequest, "invalid autonomy")
		}
	}

	// Parse nullable projectId / spawnerId: absent = leave, null = clear, string = set.
	projectIDPtr, clearProject, err := parseNullableString(body.ProjectID)
	if err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "projectId must be a string or null")
	}
	spawnerIDPtr, clearSpawner, err := parseNullableString(body.SpawnerID)
	if err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "spawnerId must be a string or null")
	}

	if projectIDPtr != nil {
		if h.projectRepo == nil {
			return apierr.NewAppError(http.StatusInternalServerError, "project repo not configured")
		}
		if _, err := h.projectRepo.GetByID(r.Context(), *projectIDPtr); err != nil {
			if ent.IsNotFound(err) {
				return apierr.NewAppError(http.StatusNotFound, "project not found")
			}
			return fmt.Errorf("tasks.update.projectLookup: %w", err)
		}
	}
	if spawnerIDPtr != nil {
		if h.spawnerRepo == nil {
			return apierr.NewAppError(http.StatusInternalServerError, "spawner repo not configured")
		}
		if _, err := h.spawnerRepo.GetByID(r.Context(), *spawnerIDPtr); err != nil {
			if ent.IsNotFound(err) {
				return apierr.NewAppError(http.StatusNotFound, "spawner not found")
			}
			return fmt.Errorf("tasks.update.spawnerLookup: %w", err)
		}
	}

	clampNegativeBudget(body.CostBudgetCents)
	clampNegativeBudget(body.TokenBudget)
	updated, err := h.taskRepo.Update(r.Context(), id, repo.UpdateTaskInput{
		Title:           body.Title,
		Description:     body.Description,
		Priority:        body.Priority,
		SilverBullet:    body.SilverBullet,
		MaxIterations:   body.MaxIterations,
		CostBudgetCents: body.CostBudgetCents,
		TokenBudget:     body.TokenBudget,
		ProjectID:       projectIDPtr,
		SpawnerID:       spawnerIDPtr,
		ClearProjectID:  clearProject,
		ClearSpawnerID:  clearSpawner,
		Autonomy:        body.Autonomy,
	})
	if err != nil {
		return fmt.Errorf("tasks.update: %w", err)
	}
	h.broadcastEnrichedUpdate(r.Context(), id)

	// cwd_not_in_project warning applies when either cwd or projectId was in
	// this PATCH body. The PATCH does not actually mutate cwd (no setter
	// exposed yet), so we compare the post-update task's cwd against the
	// effective project's folders.
	_ = existing
	cwdInPatch := body.Cwd != nil
	projectInPatch := projectIDPtr != nil || clearProject
	if cwdInPatch || projectInPatch {
		if updated.ProjectID != nil && h.cwdNotInProjectWarning(r.Context(), *updated.ProjectID, updated.Cwd) {
			enriched, _ := EnrichTask(r.Context(), updated, h.srRepo, h.permRepo)
			h.applyRefineStatus(enriched, enriched.ID)
			return jsonReply(w, http.StatusOK, map[string]any{
				"task":    enriched,
				"warning": "cwd_not_in_project",
			})
		}
	}
	return jsonReply(w, http.StatusOK, updated)
}

// parseNullableString decodes a PATCH field that may be absent, JSON null, or
// a string. Returns (value, clear, err): clear=true when JSON null was sent.
func parseNullableString(raw json.RawMessage) (*string, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	s := string(raw)
	if s == "null" {
		return nil, true, nil
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false, err
	}
	return &v, false, nil
}

// rankTask repositions a task within its stage column. The body carries the IDs
// of the cards immediately above (before) and below (after) the drop target;
// either may be empty when dropping at a column edge. The server computes the
// midpoint rank so concurrent drops stay race-safe.
func (h *Handler) rankTask(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	var body struct {
		Before string `json:"before"`
		After  string `json:"after"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if body.Before == id || body.After == id {
		return apierr.NewAppError(http.StatusBadRequest, "before/after must not equal the task being ranked")
	}
	updated, err := h.taskRepo.RerankBetween(r.Context(), id, body.Before, body.After)
	if err != nil {
		if ent.IsNotFound(err) {
			return apierr.ErrNotFound
		}
		return fmt.Errorf("tasks.rankTask: %w", err)
	}
	h.broadcastEnrichedUpdate(r.Context(), id)
	enriched, err := EnrichTask(r.Context(), updated, h.srRepo, h.permRepo)
	if err != nil {
		return fmt.Errorf("tasks.rankTask.enrich: %w", err)
	}
	h.applyRefineStatus(enriched, id)
	return jsonReply(w, http.StatusOK, enriched)
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
	_ = h.auditRepo.RecordTaskAudit(r.Context(), id, nil, "retry_requested", "task:"+id, map[string]any{"actor": "user", "stage": latest.Stage, "iteration": latest.Iteration})
	sr, err := h.orchestrator.RequeueForUser(r.Context(), id, body.AdditionalPrompt)
	if err != nil {
		return fmt.Errorf("tasks.retry: %w", err)
	}
	if sr == nil {
		return apierr.NewAppError(http.StatusConflict, "task could not progress")
	}
	h.broadcastEnrichedUpdate(r.Context(), id)
	return jsonReply(w, http.StatusAccepted, sr)
}

func (h *Handler) advance(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	res, err := Advance(r.Context(), AdvanceDeps{
		TaskRepo:     h.taskRepo,
		SRRepo:       h.srRepo,
		PermRepo:     h.permRepo,
		AuditRepo:    h.auditRepo,
		Orchestrator: h.orchestrator,
		RefineReader: h.refineReader,
	}, id)
	if err != nil {
		return fmt.Errorf("tasks.advance: %w", err)
	}
	_ = h.auditRepo.RecordTaskAudit(r.Context(), id, nil, "task_advanced", "task:"+id, map[string]any{"actor": "user", "primary": res.Primary, "dispatched": res.Dispatched})
	h.broadcastEnrichedUpdate(r.Context(), id)
	return jsonReply(w, http.StatusOK, res)
}

func (h *Handler) hold(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	t, err := h.taskRepo.GetByID(r.Context(), id)
	if err != nil {
		return apierr.ErrNotFound
	}
	if t.CurrentStage == "done" || t.CurrentStage == "cancelled" || t.CurrentStage == "on_hold" {
		return apierr.NewAppError(http.StatusBadRequest, "task is already "+t.CurrentStage)
	}
	onHold := "on_hold"
	updated, err := h.taskRepo.Update(r.Context(), id, repo.UpdateTaskInput{CurrentStage: &onHold})
	if err != nil {
		return fmt.Errorf("tasks.hold: %w", err)
	}
	_ = h.auditRepo.RecordTaskAudit(r.Context(), id, nil, "task_held", "task:"+id, map[string]any{"actor": "user", "fromStage": t.CurrentStage})
	h.broadcastEnrichedUpdate(r.Context(), id)
	return jsonReply(w, http.StatusOK, updated)
}

func (h *Handler) resume(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	t, err := h.taskRepo.GetByID(r.Context(), id)
	if err != nil {
		return apierr.ErrNotFound
	}
	var body struct {
		AdditionalPrompt string `json:"additionalPrompt"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	// When on_hold: move back to implementation before re-queuing.
	if t.CurrentStage == "on_hold" {
		impl := "implementation"
		if _, err := h.taskRepo.Update(r.Context(), id, repo.UpdateTaskInput{CurrentStage: &impl}); err != nil {
			return fmt.Errorf("tasks.resume.unstage: %w", err)
		}
	}
	sr, err := h.orchestrator.ResumeFromUser(r.Context(), id, body.AdditionalPrompt)
	if err != nil {
		return fmt.Errorf("tasks.resume: %w", err)
	}
	if sr == nil {
		return apierr.NewAppError(http.StatusConflict, "task cannot be resumed (terminal or missing)")
	}
	_ = h.auditRepo.RecordTaskAudit(r.Context(), id, nil, "task_resumed", "task:"+id, map[string]any{"actor": "user", "hadPrompt": body.AdditionalPrompt != ""})
	h.broadcastEnrichedUpdate(r.Context(), id)
	return jsonReply(w, http.StatusAccepted, sr)
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
	// Object-level authz: the request must belong to the task in the URL,
	// otherwise the nested {taskId}/{reqID} path is not an enforced scope.
	pr, err := h.permRepo.GetPermissionRequest(r.Context(), reqID)
	if err != nil {
		if ent.IsNotFound(err) {
			return apierr.ErrNotFound
		}
		return fmt.Errorf("tasks.resolvePermissionRequest.get: %w", err)
	}
	sr, err := h.srRepo.GetByID(r.Context(), pr.StageRunID)
	if err != nil || sr.TaskID != id {
		return apierr.ErrNotFound
	}
	if err := h.permRepo.ResolvePermissionRequest(r.Context(), reqID, body.Outcome); err != nil {
		return fmt.Errorf("tasks.resolvePermissionRequest: %w", err)
	}
	resolved, err := h.permRepo.GetPermissionRequest(r.Context(), reqID)
	if err != nil {
		return fmt.Errorf("tasks.resolvePermissionRequest.get: %w", err)
	}
	if body.Outcome == "granted" {
		entries := []repo.GrantEntry{{Tool: pr.Tool, Pattern: pr.Pattern}}
		if _, errs := h.grantValidatedEntries(r.Context(), id, entries); len(errs) > 0 {
			slog.Warn("resolvePermissionRequest: grant failed", "taskID", id, "errs", errs)
		}
		if _, err := h.orchestrator.ResumeFromUser(r.Context(), id, ""); err != nil {
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
	sse.WriteHeaders(w)
	flusher.Flush()

	sub := h.broadcaster.Subscribe()
	defer h.broadcaster.Unsubscribe(sub)

	for {
		select {
		case data, ok := <-sub:
			if !ok {
				return
			}
			// data is a fully-formed SSE frame from the broadcaster — write raw.
			w.Write(data) //nolint:errcheck
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
