package mcp

import (
	"context"
	"log/slog"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// StageRunLookup and TaskLookup are the two reads CallerResolver needs. They
// are declared here, narrow, rather than taking the full repo interfaces: the
// resolver depends on the methods it calls and nothing else.
type StageRunLookup interface {
	GetByID(ctx context.Context, id string) (*ent.StageRun, error)
}

type TaskLookup interface {
	GetByID(ctx context.Context, id string) (*ent.Task, error)
}

// CallerResolver turns the stage run on a request's MCP credential into the
// capability contexts that request resolves against.
//
// A user key resolves to nothing, which is why an unattributed call behaves
// exactly as it did before this existed — capability.Decide drops every grant
// whose context the request does not name, so "no contexts" means the scope
// chain alone decides, as it always did.
type CallerResolver struct {
	StageRuns StageRunLookup
	Tasks     TaskLookup
}

// Contexts resolves the caller's chain: the task the stage run belongs to, and
// the routine that materialized that task when there is one.
//
// Every failure resolves to no contexts rather than a partial chain: the safe
// direction is the one a machine-wide key already takes.
func (r CallerResolver) Contexts(ctx context.Context) []capability.Context {
	info := AuthFromContext(ctx)
	if info == nil || info.StageRunID == "" || r.StageRuns == nil || r.Tasks == nil {
		return nil
	}
	run, err := r.StageRuns.GetByID(ctx, info.StageRunID)
	if err != nil {
		slog.Debug("mcp: stage run for credential not found", "stageRun", info.StageRunID, "err", err)
		return nil
	}
	task, err := r.Tasks.GetByID(ctx, run.TaskID)
	if err != nil {
		slog.Debug("mcp: task for stage run not found", "stageRun", run.ID, "task", run.TaskID, "err", err)
		return nil
	}
	out := []capability.Context{{Kind: repo.GrantContextTask, Ref: task.ID}}
	if task.RoutineID != nil && *task.RoutineID != "" {
		out = append(out, capability.Context{Kind: repo.GrantContextRoutine, Ref: *task.RoutineID})
	}
	return out
}
