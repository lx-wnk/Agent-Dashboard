package tasks

import (
	"context"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

// EnrichedTask extends Task with computed UI fields.
type EnrichedTask struct {
	*ent.Task
	NeedsUser                   bool    `json:"needsUser"`
	LatestStageRunStatus        *string `json:"latestStageRunStatus"`
	CurrentIteration            int     `json:"currentIteration"`
	ActiveSessionID             *string `json:"activeSessionId"`
	ActivePID                   *int    `json:"activePid"`
	BlockedByPendingPermissions bool    `json:"blockedByPendingPermissions"`
}

func EnrichTask(ctx context.Context, t *ent.Task, srRepo repo.StageRunRepo, permRepo repo.PermissionRepo) (*EnrichedTask, error) {
	latest, _ := srRepo.GetLatestForTask(ctx, t.ID)
	return enrichOne(ctx, t, latest, permRepo)
}

func EnrichTasksBulk(ctx context.Context, tasks []*ent.Task, srRepo repo.StageRunRepo, permRepo repo.PermissionRepo) ([]*EnrichedTask, error) {
	if len(tasks) == 0 {
		return []*EnrichedTask{}, nil
	}
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	latestMap, err := srRepo.GetLatestForTasks(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make([]*EnrichedTask, len(tasks))
	for i, t := range tasks {
		enriched, err := enrichOne(ctx, t, latestMap[t.ID], permRepo)
		if err != nil {
			return nil, err
		}
		result[i] = enriched
	}
	return result, nil
}

func enrichOne(ctx context.Context, t *ent.Task, latest *ent.StageRun, permRepo repo.PermissionRepo) (*EnrichedTask, error) {
	latestBelongsToCurrent := latest != nil && latest.Stage == t.CurrentStage
	var latestStatus *string
	currentIteration := 0
	if latestBelongsToCurrent {
		latestStatus = &latest.Status
		currentIteration = latest.Iteration
	}

	var pendingPermsCount int
	if latestBelongsToCurrent {
		n, _ := permRepo.CountForStageRun(ctx, latest.ID)
		pendingPermsCount = n
	}

	hasPendingPermissions := latestStatus != nil && *latestStatus == "running" && pendingPermsCount > 0
	isTerminal := latestStatus != nil && (*latestStatus == "failed" || *latestStatus == "done")
	isZombieAwait := latestStatus != nil && *latestStatus == "awaiting_user" && latest != nil &&
		(latest.Pid == nil || !pipeline.IsPidAlive(*latest.Pid))
	blockedByPendingPermissions := (isTerminal || isZombieAwait) && pendingPermsCount > 0

	needsUser := t.CurrentStage == "on_hold" ||
		(latestStatus != nil && (*latestStatus == "awaiting_user" || *latestStatus == "on_hold" || *latestStatus == "failed")) ||
		hasPendingPermissions || blockedByPendingPermissions

	var activeSessionID *string
	var activePID *int
	if latest != nil {
		activeSessionID = latest.SessionID
		if latest.Status == "running" {
			activePID = latest.Pid
		}
	}

	return &EnrichedTask{
		Task:                        t,
		NeedsUser:                   needsUser,
		LatestStageRunStatus:        latestStatus,
		CurrentIteration:            currentIteration,
		ActiveSessionID:             activeSessionID,
		ActivePID:                   activePID,
		BlockedByPendingPermissions: blockedByPendingPermissions,
	}, nil
}
