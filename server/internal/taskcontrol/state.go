// Package taskcontrol computes the set of valid UI/MCP actions for a task
// given its current stage, run status, and permission state. It is the single
// source of truth for "what can the user do with this task right now?".
package taskcontrol

// TaskState is a lightweight projection of the fields needed to determine
// which actions are valid. All fields are plain strings/ints so ComputeActions
// stays pure (no DB/IO) and is trivially testable.
type TaskState struct {
	// Stage is the task's current pipeline stage (e.g. "concept", "implementation").
	Stage string
	// RunStatus is the status of the latest StageRun that belongs to the current
	// stage, or "" when no run exists yet.
	RunStatus string
	// RefineStatus is the status returned by the refine runner ("none", "refining",
	// "draft_ready", "failed"), or "" when the refine feature is not configured.
	RefineStatus string
	// PendingPerms is the count of unresolved permission requests on the latest run.
	PendingPerms int
	// NeedsUser mirrors EnrichedTask.NeedsUser — true when the task requires a
	// human action before the pipeline can make progress.
	NeedsUser bool
}

// FromFields constructs a TaskState from the individual components already
// available in the enrich layer. This is the canonical constructor: callers
// must not build TaskState literals directly so we can add fields here without
// breaking callsites.
func FromFields(stage, runStatus, refineStatus string, pendingPerms int, needsUser bool) TaskState {
	return TaskState{
		Stage:        stage,
		RunStatus:    runStatus,
		RefineStatus: refineStatus,
		PendingPerms: pendingPerms,
		NeedsUser:    needsUser,
	}
}
