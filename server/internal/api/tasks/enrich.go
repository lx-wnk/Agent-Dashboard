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
	var pendingCount int
	if latest != nil && latest.Stage == t.CurrentStage {
		pendingCount, _ = permRepo.CountForStageRun(ctx, latest.ID)
	}
	return enrichOne(t, latest, pendingCount)
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

	result := make([]*EnrichedTask, len(tasks))
	for i, t := range tasks {
		latest := latestMap[t.ID]
		var pendingCount int
		if latest != nil && latest.Stage == t.CurrentStage {
			pendingCount = pendingCounts[latest.ID]
		}
		enriched, err := enrichOne(t, latest, pendingCount)
		if err != nil {
			return nil, err
		}
		result[i] = enriched
	}
	return result, nil
}

func enrichOne(t *ent.Task, latest *ent.StageRun, pendingPermsCount int) (*EnrichedTask, error) {
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
