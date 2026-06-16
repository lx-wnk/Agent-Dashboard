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

// FinalizeCompletedAsyncRunsForTest exposes finalizeCompletedAsyncRuns for testing.
func (o *PipelineOrchestrator) FinalizeCompletedAsyncRunsForTest(ctx context.Context, runs []*ent.StageRun) error {
	return o.finalizeCompletedAsyncRuns(ctx, runs)
}

// SweepRequeueableRunsForTest exposes sweepRequeueableRuns for testing.
func (o *PipelineOrchestrator) SweepRequeueableRunsForTest(ctx context.Context) error {
	return o.sweepRequeueableRuns(ctx)
}

// SweepOrphanRunsForTest exposes sweepOrphanRuns for testing.
func (o *PipelineOrchestrator) SweepOrphanRunsForTest(ctx context.Context, allRunning []*ent.StageRun) error {
	return o.sweepOrphanRuns(ctx, allRunning)
}

// PickNextTasksForFreeSlots exposes pickNextTasksForFreeSlots for testing.
func (o *PipelineOrchestrator) PickNextTasksForFreeSlots(ctx context.Context, allRunning []*ent.StageRun) {
	o.pickNextTasksForFreeSlots(ctx, allRunning)
}

// BuildStageUserPromptForTest exposes buildStageUserPrompt for testing.
var BuildStageUserPromptForTest = buildStageUserPrompt

// ResumeContinueInstructionForTest exposes resumeContinueInstruction for testing.
const ResumeContinueInstructionForTest = resumeContinueInstruction
