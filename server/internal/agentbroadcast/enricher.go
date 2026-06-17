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
// agent.SessionID → StageRunRepo.GetBySessionID → stageRun.TaskID →
// TaskRepo.GetByID → task.Title.
//
// When perms is non-nil, it also attaches any pending permission requests for
// the resolved stage run to PendingPermissions.
//
// The crossing is one-way (pipeline → agent annotation) and best-effort: nil
// repos, a session with no stage_run (the common case for ad-hoc sessions), or
// any query error leaves PipelineTaskID/Title/PendingPermissions empty without
// failing the scan. This is the Go re-implementation of the former TS-era
// enrichWithPipelineTask.
//
// agentbroadcast is a peer of merger and may import db/repo, which keeps merger
// itself free of any db dependency (Go layer direction).
func NewPipelineTaskEnricher(stageRuns repo.StageRunRepo, tasks repo.TaskRepo, perms repo.PermissionRepo) merger.Enricher {
	return func(ctx context.Context, agents []sdk.Agent) {
		if stageRuns == nil || tasks == nil {
			return
		}
		// Iterate by index so writes land on the slice elements, not on copies.
		for i := range agents {
			sid := agents[i].SessionID
			if sid == "" {
				continue
			}
			sr, err := stageRuns.GetBySessionID(ctx, sid)
			if err != nil {
				// Not-found is the dominant, expected case (most sessions are not
				// pipeline stage runs) — only surface genuine query failures.
				if !ent.IsNotFound(err) {
					slog.Debug("pipeline enricher: stage_run lookup failed", "sessionId", sid, "err", err)
				}
				continue
			}
			agents[i].PipelineTaskID = sr.TaskID

			task, err := tasks.GetByID(ctx, sr.TaskID)
			if err != nil {
				if !ent.IsNotFound(err) {
					slog.Debug("pipeline enricher: task lookup failed", "taskId", sr.TaskID, "err", err)
				}
				continue
			}
			agents[i].PipelineTaskTitle = task.Title

			if perms != nil {
				pendingReqs, perr := perms.ListPendingForStageRun(ctx, sr.ID)
				if perr != nil {
					slog.Debug("pipeline enricher: pending permissions lookup failed", "stageRunId", sr.ID, "err", perr)
				} else if len(pendingReqs) > 0 {
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
}
