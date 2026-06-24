package pipeline

import (
	"context"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

// ExportedWriteSettingsFile exposes writeSettingsFile for spawner tests.
func ExportedWriteSettingsFile(autonomy, cwd string, perms []*ent.TaskPermission, enableChannel, allowGitPush bool) (string, bool, bool, error) {
	return writeSettingsFile(autonomy, cwd, perms, enableChannel, allowGitPush)
}

// SortPickCandidatesForTest exposes sortPickCandidates for testing.
var SortPickCandidatesForTest = sortPickCandidates

// DecideCompletedTransitionForTest exposes decideCompletedTransition for testing.
func (o *PipelineOrchestrator) DecideCompletedTransitionForTest(
	ctx context.Context, task *ent.Task, run *ent.StageRun, output map[string]any,
) StageTransition {
	return o.decideCompletedTransition(ctx, task, run, output)
}

// IsRateLimitErrorForTest exposes isRateLimitError for testing.
var IsRateLimitErrorForTest = isRateLimitError

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

// SyntheticSpawnPIDForTest exposes syntheticSpawnPID for use in test capture
// spawn functions that want to return the canonical no-op PID.
const SyntheticSpawnPIDForTest = syntheticSpawnPID

// NewAgentStageHandlerForTest creates an agentStageHandler with the given spawn
// function injected, so unit tests can capture SpawnAgentOptions without running
// a real claude subprocess.
func NewAgentStageHandlerForTest(stage string, spawnFn func(SpawnAgentOptions) (SpawnResult, error)) StageHandler {
	return &agentStageHandler{
		stage:       stage,
		buildPrompt: func(_ *StageContext) PromptBundle { return PromptBundle{UserPrompt: "test"} },
		spawnFn:     spawnFn,
	}
}

// SetResolveSpawner injects a SpawnerResolverFunc for testing spawner-override
// precedence without a full DB-backed spawner repo.
func (o *PipelineOrchestrator) SetResolveSpawner(fn SpawnerResolverFunc) {
	o.opts.ResolveSpawner = fn
}

// PlanReviewBuilderForTest exposes planReviewBuilder for unit tests.
var PlanReviewBuilderForTest = planReviewBuilder
