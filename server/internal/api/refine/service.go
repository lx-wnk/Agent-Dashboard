package refine

import (
	"context"
	"fmt"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/refine"
)

// ConfirmDeps are the subset of Deps required by Confirm.
type ConfirmDeps struct {
	Turns     repo.RefinementTurnRepo
	Tasks     repo.TaskRepo
	StageRuns repo.StageRunRepo
	Advance   func(ctx context.Context, taskID string) error
}

// Confirm freezes the concept from refinement turns onto the task and advances
// it past the concept stage. It is the shared implementation called by both
// the REST handler and the MCP approve_spec tool.
// Returns the updated task on success.
func Confirm(ctx context.Context, d ConfirmDeps, taskID string) (*ent.Task, error) {
	backlog := "backlog"
	update := repo.UpdateTaskInput{CurrentStage: &backlog}
	if d.Turns != nil {
		if turns, err := d.Turns.ListForTask(ctx, taskID, 0); err == nil {
			if concept, ok := refine.ExtractConcept(turns); ok {
				if meta := concept.Metadata(); len(meta) > 0 {
					update.Metadata = meta
				}
				if concept.RefinedTitle != "" {
					update.Title = &concept.RefinedTitle
				}
				if concept.SourceBranch != "" {
					n, err := d.Tasks.CountActiveBySourceBranch(ctx, concept.SourceBranch, taskID)
					if err != nil {
						return nil, err
					}
					if n > 0 {
						return nil, fmt.Errorf("source_branch %q already in use by another active task", concept.SourceBranch)
					}
					update.SourceBranch = &concept.SourceBranch
				}
				if concept.TargetBranch != "" {
					update.TargetBranch = &concept.TargetBranch
				}
			}
		}
	}

	phase := "confirmed"
	if _, err := d.Turns.Create(ctx, repo.CreateTurnInput{
		TaskID:  taskID,
		Role:    "assistant",
		Content: "confirmed",
		Phase:   &phase,
	}); err != nil {
		return nil, err
	}

	if d.StageRuns != nil {
		now := time.Now()
		done := "done"
		if sr, err := d.StageRuns.GetLatestByTaskAndStage(ctx, taskID, "concept"); err == nil && sr != nil {
			_, _ = d.StageRuns.Update(ctx, sr.ID, repo.UpdateStageRunInput{
				Status:  &done,
				EndedAt: &now,
			})
		}
	}
	// The task update is the load-bearing write (stage move + concept freeze);
	// fail loudly rather than advancing a task whose spec never landed.
	if d.Tasks != nil {
		if _, err := d.Tasks.Update(ctx, taskID, update); err != nil {
			return nil, fmt.Errorf("confirm: apply concept to task: %w", err)
		}
	}
	if d.Advance != nil {
		_ = d.Advance(ctx, taskID)
	}

	return d.Tasks.GetByID(ctx, taskID)
}
