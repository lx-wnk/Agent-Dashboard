package refine

import (
	"context"
	"sync"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// Run status values surfaced to the UI via the enriched task.
const (
	StatusIdle    = "idle"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
)

// SpawnFunc spawns a refinement turn and returns a line stream. Matches
// RunRefinementTurn so the real spawner is injectable (and stubbable in tests).
type SpawnFunc func(ctx context.Context, cfg SpawnConfig, sp *ent.Spawner) (<-chan string, error)

type runState struct {
	status string
	errMsg string
}

// Runner owns refinement runs: it spawns claude in a background goroutine
// (decoupled from any HTTP request), tracks per-task status in memory, and
// persists the assistant turn on completion.
type Runner struct {
	turns repo.RefinementTurnRepo
	spawn SpawnFunc

	mu          sync.Mutex
	runs        map[string]*runState
	onRunChange func(taskID string)
}

// NewRunner builds a Runner. spawn defaults to RunRefinementTurn when nil.
func NewRunner(turns repo.RefinementTurnRepo, spawn SpawnFunc) *Runner {
	if spawn == nil {
		spawn = RunRefinementTurn
	}
	return &Runner{turns: turns, spawn: spawn, runs: make(map[string]*runState)}
}

// SetOnRunChange late-binds the status-change callback (the composition root
// wires this to a Task-SSE broadcast after the tasks handler is constructed).
func (r *Runner) SetOnRunChange(fn func(taskID string)) { r.onRunChange = fn }

// State returns the current status and last error for a task.
func (r *Runner) State(taskID string) (status, errMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.runs[taskID]
	if !ok {
		return StatusIdle, ""
	}
	return s.status, s.errMsg
}

// IsRunning reports whether a run is currently in flight for the task.
func (r *Runner) IsRunning(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.runs[taskID]
	return ok && s.status == StatusRunning
}

func (r *Runner) setState(taskID, status, errMsg string) {
	r.mu.Lock()
	r.runs[taskID] = &runState{status: status, errMsg: errMsg}
	r.mu.Unlock()
	if r.onRunChange != nil {
		r.onRunChange(taskID)
	}
}
