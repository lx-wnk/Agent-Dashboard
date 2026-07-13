// server/internal/agentbroadcast/enricher.go
package agentbroadcast

import (
	"context"
	"log/slog"
	"time"

	sdk "github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
)

// NewPipelineTaskEnricher returns a merger.Enricher that annotates each agent
// with its linked pipeline task (ID + title), resolved read-only from SQLite:
// agent.SessionID → StageRunRepo.ListBySessionIDs → stageRun.TaskID →
// TaskRepo.ListByIDs → task.Title.
//
// When perms is non-nil, it also attaches any pending permission requests for
// the resolved stage run to PendingPermissions.
//
// All three lookups are batched to one query each per tick (session IDs →
// stage runs, resolved task IDs → tasks, resolved stage run IDs → pending
// permissions) instead of per-agent round-trips, then joined in-memory.
//
// The crossing is one-way (pipeline → agent annotation) and best-effort: nil
// repos, a session with no stage_run (the common case for ad-hoc sessions), or
// any query error leaves PipelineTaskID/Title/PendingPermissions empty without
// failing the scan.
//
// agentbroadcast is a peer of merger and may import db/repo, which keeps merger
// itself free of any db dependency (Go layer direction).
func NewPipelineTaskEnricher(stageRuns repo.StageRunRepo, tasks repo.TaskRepo, perms repo.PermissionRepo) merger.Enricher {
	return func(ctx context.Context, agents []sdk.Agent) {
		if stageRuns == nil || tasks == nil {
			return
		}

		sessionIDs := make([]string, 0, len(agents))
		seen := make(map[string]struct{}, len(agents))
		for i := range agents {
			sid := agents[i].SessionID
			if sid == "" {
				continue
			}
			if _, ok := seen[sid]; ok {
				continue
			}
			seen[sid] = struct{}{}
			sessionIDs = append(sessionIDs, sid)
		}
		if len(sessionIDs) == 0 {
			return
		}

		runs, err := stageRuns.ListBySessionIDs(ctx, sessionIDs)
		if err != nil {
			slog.Debug("pipeline enricher: stage_run batch lookup failed", "err", err)
			return
		}

		stageRunBySession := make(map[string]*ent.StageRun, len(runs))
		taskIDSeen := make(map[string]struct{}, len(runs))
		taskIDs := make([]string, 0, len(runs))
		stageRunIDs := make([]string, 0, len(runs))
		for _, sr := range runs {
			if sr.SessionID == nil {
				continue
			}
			stageRunBySession[*sr.SessionID] = sr
			stageRunIDs = append(stageRunIDs, sr.ID)
			if _, ok := taskIDSeen[sr.TaskID]; !ok {
				taskIDSeen[sr.TaskID] = struct{}{}
				taskIDs = append(taskIDs, sr.TaskID)
			}
		}

		taskByID := make(map[string]*ent.Task, len(taskIDs))
		taskList, err := tasks.ListByIDs(ctx, taskIDs)
		if err != nil {
			slog.Debug("pipeline enricher: task batch lookup failed", "err", err)
		} else {
			for _, t := range taskList {
				taskByID[t.ID] = t
			}
		}

		pendingByStageRun := make(map[string][]*ent.PermissionRequest)
		if perms != nil {
			pendingReqs, perr := perms.ListPendingForStageRuns(ctx, stageRunIDs)
			if perr != nil {
				slog.Debug("pipeline enricher: pending permissions batch lookup failed", "err", perr)
			} else {
				for _, req := range pendingReqs {
					pendingByStageRun[req.StageRunID] = append(pendingByStageRun[req.StageRunID], req)
				}
			}
		}

		// Iterate by index so writes land on the slice elements, not on copies.
		for i := range agents {
			sr, ok := stageRunBySession[agents[i].SessionID]
			if !ok {
				continue
			}
			agents[i].PipelineTaskID = sr.TaskID

			if task, ok := taskByID[sr.TaskID]; ok {
				agents[i].PipelineTaskTitle = task.Title
			}

			if pendingReqs := pendingByStageRun[sr.ID]; len(pendingReqs) > 0 {
				pp := make([]sdk.PendingPermission, len(pendingReqs))
				for j, req := range pendingReqs {
					pp[j] = sdk.PendingPermission{
						ID:          req.ID,
						Tool:        req.Tool,
						Pattern:     req.Pattern,
						Reason:      req.Reason,
						RequestedAt: req.RequestedAt.UTC().Format(time.RFC3339),
					}
				}
				agents[i].PendingPermissions = pp
			}
		}
	}
}
