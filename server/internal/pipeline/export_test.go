package pipeline

import (
	"context"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

// SortPickCandidatesForTest exposes sortPickCandidates for testing.
var SortPickCandidatesForTest = sortPickCandidates

// DecideCompletedTransitionForTest exposes decideCompletedTransition for testing.
func (o *PipelineOrchestrator) DecideCompletedTransitionForTest(
	ctx context.Context, task *ent.Task, run *ent.StageRun, output map[string]any,
) StageTransition {
	return o.decideCompletedTransition(ctx, task, run, output)
}

// ClassifyInfraForTest exposes classifyInfra for testing.
var ClassifyInfraForTest = classifyInfra
