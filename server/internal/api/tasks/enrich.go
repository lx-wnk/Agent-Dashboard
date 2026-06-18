package tasks

import (
	"context"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/rawrepo"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/lx-wnk/agent-dashboard/server/internal/taskcontrol"
)

// EnrichedTask is the API response shape for a task. All fields use camelCase
// JSON keys so they match the TypeScript PipelineTask interface without a
// client-side transform. Do not embed *ent.Task here — ent generates snake_case
// JSON tags that would silently shadow the camelCase keys below.
type EnrichedTask struct {
	ID                  string                 `json:"id"`
	Slug                string                 `json:"slug"`
	Title               string                 `json:"title"`
	Description         *string                `json:"description"`
	Cwd                 string                 `json:"cwd"`
	WorktreePath        *string                `json:"worktreePath"`
	SourceBranch        *string                `json:"sourceBranch"`
	TargetBranch        *string                `json:"targetBranch"`
	CurrentStage        string                 `json:"currentStage"`
	Priority            string                 `json:"priority"`
	Autonomy            string                 `json:"autonomy"`
	UserID              *string                `json:"userId"`
	ParentTaskID        *string                `json:"parentTaskId"`
	ProjectID           *string                `json:"projectId"`
	SpawnerID           *string                `json:"spawnerId"`
	MaxIterations       int                    `json:"maxIterations"`
	TokenBudget         *int                   `json:"tokenBudget"`
	CostBudgetCents     *int                   `json:"costBudgetCents"`
	StageTimeoutSeconds int                    `json:"stageTimeoutSeconds"`
	SilverBullet        bool                   `json:"silverBullet"`
	Rank                *float64               `json:"rank"`
	Metadata            map[string]interface{} `json:"metadata"`
	CreatedAt           time.Time              `json:"createdAt"`
	UpdatedAt           time.Time              `json:"updatedAt"`
	// Computed fields — not stored in DB.
	NeedsUser                   bool       `json:"needsUser"`
	LatestStageRunStatus        *string    `json:"latestStageRunStatus"`
	AutoRetryCount              *int       `json:"autoRetryCount,omitempty"`
	NextRetryAt                 *time.Time `json:"nextRetryAt,omitempty"`
	RefineStatus                *string    `json:"refineStatus,omitempty"`
	RefineError                 *string    `json:"refineError,omitempty"`
	CurrentIteration            int        `json:"currentIteration"`
	ActiveSessionID             *string    `json:"activeSessionId"`
	ActivePID                   *int       `json:"activePid"`
	BlockedByPendingPermissions bool                  `json:"blockedByPendingPermissions"`
	AvailableActions            []taskcontrol.Action  `json:"availableActions"`
}

// pidAliveMemo returns a func(int) bool that wraps pipeline.IsPidAlive and caches
// each distinct pid's liveness for the lifetime of the returned closure. A single
// memo is shared across one enrich pass so a pid reused by several sibling tasks
// costs exactly one syscall. The closure is not safe for concurrent use — build
// one per enrich call.
func pidAliveMemo() func(int) bool {
	return memoizeProbe(pipeline.IsPidAlive)
}

// memoizeProbe caches each distinct pid's probe result for the lifetime of the
// returned closure. Split out from pidAliveMemo so the dedup logic is unit-testable
// with a counting probe.
func memoizeProbe(probe func(int) bool) func(int) bool {
	cache := make(map[int]bool)
	return func(pid int) bool {
		if alive, ok := cache[pid]; ok {
			return alive
		}
		alive := probe(pid)
		cache[pid] = alive
		return alive
	}
}

func EnrichTask(ctx context.Context, t *ent.Task, srRepo repo.StageRunRepo, permRepo repo.PermissionRepo) (*EnrichedTask, error) {
	latest, _ := srRepo.GetLatestForTask(ctx, t.ID)
	var pendingCount int
	if latest != nil && latest.Stage == t.CurrentStage {
		pendingCount, _ = permRepo.CountForStageRun(ctx, latest.ID)
	}
	return enrichOne(t, latest, pendingCount, pidAliveMemo())
}

// EnrichTasksBulk enriches a slice of tasks with their latest stage-run status
// and pending-permission counts. It uses the window-function bulk repo
// (rawrepo.StageRunBulkRepo.LatestPerTask) for an exact per-task latest query,
// which is correct regardless of iteration count. srRepo is retained for the
// single-task EnrichTask path; it is unused here.
func EnrichTasksBulk(ctx context.Context, tasks []*ent.Task, _ repo.StageRunRepo, permRepo repo.PermissionRepo, bulkRepo rawrepo.StageRunBulkRepo) ([]*EnrichedTask, error) {
	if len(tasks) == 0 {
		return []*EnrichedTask{}, nil
	}
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	latestMap, err := bulkRepo.LatestPerTask(ctx, ids)
	if err != nil {
		return nil, err
	}

	// Collect stage run IDs that belong to the current stage — only those need a count.
	srIDs := make([]string, 0, len(tasks))
	for _, t := range tasks {
		if sr := latestMap[t.ID]; sr != nil && sr.Stage == t.CurrentStage {
			srIDs = append(srIDs, sr.ID)
		}
	}
	pendingCounts, err := permRepo.CountForStageRunsBulk(ctx, srIDs)
	if err != nil {
		return nil, err
	}

	// One memo shared across the whole slice so a pid reused by sibling tasks
	// hits the liveness syscall once per tick.
	isAlive := pidAliveMemo()

	result := make([]*EnrichedTask, len(tasks))
	for i, t := range tasks {
		latest := latestMap[t.ID]
		var pendingCount int
		if latest != nil && latest.Stage == t.CurrentStage {
			pendingCount = pendingCounts[latest.ID]
		}
		enriched, err := enrichOne(t, latest, pendingCount, isAlive)
		if err != nil {
			return nil, err
		}
		result[i] = enriched
	}
	return result, nil
}

func enrichOne(t *ent.Task, latest *ent.StageRun, pendingPermsCount int, isAlive func(int) bool) (*EnrichedTask, error) {
	latestBelongsToCurrent := latest != nil && latest.Stage == t.CurrentStage
	var latestStatus *string
	currentIteration := 0
	if latestBelongsToCurrent {
		latestStatus = &latest.Status
		currentIteration = latest.Iteration
	}

	hasPendingPermissions := latestStatus != nil && *latestStatus == "running" && pendingPermsCount > 0
	isTerminal := latestStatus != nil && (*latestStatus == "failed" || *latestStatus == "done")
	isZombieAwait := latestStatus != nil && *latestStatus == "awaiting_user" && latest != nil &&
		(latest.Pid == nil || !isAlive(*latest.Pid))
	blockedByPendingPermissions := (isTerminal || isZombieAwait) && pendingPermsCount > 0

	needsUser := t.CurrentStage == "on_hold" ||
		(latestStatus != nil && (*latestStatus == "awaiting_user" || *latestStatus == "on_hold" || *latestStatus == "failed")) ||
		hasPendingPermissions || blockedByPendingPermissions

	var autoRetryCount *int
	var nextRetryAt *time.Time
	if latestBelongsToCurrent {
		if latest.RetryCount > 0 {
			autoRetryCount = &latest.RetryCount
		}
		nextRetryAt = latest.NextRetryAt
	}

	var activeSessionID *string
	var activePID *int
	if latest != nil {
		activeSessionID = latest.SessionID
		if latest.Status == "running" {
			activePID = latest.Pid
		}
	}

	e := &EnrichedTask{
		ID:                          t.ID,
		Slug:                        t.Slug,
		Title:                       t.Title,
		Description:                 t.Description,
		Cwd:                         t.Cwd,
		WorktreePath:                t.WorktreePath,
		SourceBranch:                t.SourceBranch,
		TargetBranch:                t.TargetBranch,
		CurrentStage:                t.CurrentStage,
		Priority:                    t.Priority,
		Autonomy:                    t.Autonomy,
		UserID:                      t.UserID,
		ParentTaskID:                t.ParentTaskID,
		ProjectID:                   t.ProjectID,
		SpawnerID:                   t.SpawnerID,
		MaxIterations:               t.MaxIterations,
		TokenBudget:                 t.TokenBudget,
		CostBudgetCents:             t.CostBudgetCents,
		StageTimeoutSeconds:         t.StageTimeoutSeconds,
		SilverBullet:                t.SilverBullet,
		Rank:                        t.Rank,
		Metadata:                    t.Metadata,
		CreatedAt:                   t.CreatedAt,
		UpdatedAt:                   t.UpdatedAt,
		NeedsUser:                   needsUser,
		LatestStageRunStatus:        latestStatus,
		AutoRetryCount:              autoRetryCount,
		NextRetryAt:                 nextRetryAt,
		CurrentIteration:            currentIteration,
		ActiveSessionID:             activeSessionID,
		ActivePID:                   activePID,
		BlockedByPendingPermissions: blockedByPendingPermissions,
	}
	recomputeAvailableActionsWithPerms(e, pendingPermsCount)
	return e, nil
}

// RecomputeAvailableActions rebuilds AvailableActions from the current enriched
// fields. Called once by enrichOne and again by applyRefineStatus so that the
// refine runner status is always reflected in the action set.
func (e *EnrichedTask) RecomputeAvailableActions() {
	runStatus := ""
	if e.LatestStageRunStatus != nil {
		runStatus = *e.LatestStageRunStatus
	}
	refineStatus := ""
	if e.RefineStatus != nil {
		refineStatus = *e.RefineStatus
	}
	s := taskcontrol.FromFields(e.CurrentStage, runStatus, refineStatus, 0, e.NeedsUser)
	// PendingPerms is encoded in BlockedByPendingPermissions but we need the count
	// for the reason string. Reconstruct the count from the NeedsUser + blocked flag
	// only if we don't have the raw count. Since enrichOne knows the count, it sets
	// AvailableActions via the dedicated path below instead of this method.
	e.AvailableActions = taskcontrol.ComputeActions(s)
}

// recomputeAvailableActionsWithPerms rebuilds AvailableActions with the exact
// pending permission count. Used by enrichOne which has the raw count available.
func recomputeAvailableActionsWithPerms(e *EnrichedTask, pendingPermsCount int) {
	runStatus := ""
	if e.LatestStageRunStatus != nil {
		runStatus = *e.LatestStageRunStatus
	}
	refineStatus := ""
	if e.RefineStatus != nil {
		refineStatus = *e.RefineStatus
	}
	s := taskcontrol.FromFields(e.CurrentStage, runStatus, refineStatus, pendingPermsCount, e.NeedsUser)
	e.AvailableActions = taskcontrol.ComputeActions(s)
}
