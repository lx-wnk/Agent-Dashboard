package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// runProgressTaskLocked is the core of ProgressTask — called with the per-task
// mutex already held. It contains both the Re-entry Guard and the
// Lingering-Pending Gate.
func (o *PipelineOrchestrator) runProgressTaskLocked(ctx context.Context, taskID string, opts *ProgressOpts) (*ent.StageRun, error) {
	task, err := o.opts.TaskRepo.GetByID(ctx, taskID)
	if err != nil || IsTerminalStage(task.CurrentStage) {
		return nil, nil
	}

	handler := o.resolveHandler(task.CurrentStage)
	if handler == nil {
		return nil, nil
	}

	// Global runner-slot cap — agent-driven stages only.
	// hasFreeRunnerSlot issues its own DB query because it is also called
	// from route handlers, not just from the tick loop.
	if handler.RequiresAgent() && !o.hasFreeRunnerSlot(ctx, task.ID) {
		return nil, nil
	}

	// Lingering-pending gate — prevents respawn while unresolved permission_requests
	// remain on the most recent terminal or zombie-awaiting stage_run.
	// latest is declared here so ensureStageRun can reuse it and skip a redundant query.
	var latest *ent.StageRun
	if handler.RequiresAgent() {
		latest, _ = o.opts.StageRunRepo.GetLatestByTaskAndStage(ctx, task.ID, task.CurrentStage)
		if latest != nil {
			pid := 0
			if latest.Pid != nil {
				pid = *latest.Pid
			}
			isTerminal := latest.Status == "failed" || latest.Status == "done"
			isZombieAwait := latest.Status == "awaiting_user" && !IsPidAlive(pid)
			if isTerminal || isZombieAwait {
				n, _ := o.opts.PermissionRepo.CountForStageRun(ctx, latest.ID)
				if n > 0 {
					slog.Info("orchestrator: progressTask blocked by lingering permission_requests",
						"taskID", taskID, "count", n, "runID", latest.ID)
					return nil, nil
				}
			}
		}
	}

	// Auto-create a git worktree before spawning the agent.
	// Triggered when: ForceWorktrees is set (global) OR task has an explicit SourceBranch.
	// Must happen before ensureStageRun so the spawner uses the correct cwd from the first run.
	needsWorktree := handler.RequiresAgent() &&
		(task.WorktreePath == nil || *task.WorktreePath == "") &&
		(o.opts.ForceWorktrees || (task.SourceBranch != nil && *task.SourceBranch != ""))
	if needsWorktree {
		wtPath, wtBranch, wtErr := ensureTaskWorktree(task, o.opts.WorktreeRoot)
		if wtErr != nil {
			return nil, fmt.Errorf("orchestrator: ensure worktree: %w", wtErr)
		}
		upd := repo.UpdateTaskInput{WorktreePath: &wtPath}
		if task.SourceBranch == nil || *task.SourceBranch == "" {
			upd.SourceBranch = &wtBranch
		}
		if task, err = o.opts.TaskRepo.Update(ctx, task.ID, upd); err != nil {
			return nil, fmt.Errorf("orchestrator: set worktree path: %w", err)
		}
		slog.Info("orchestrator: created worktree", "taskID", taskID, "path", wtPath, "branch", wtBranch)
	}

	// latest is non-nil only for agent-driven stages; ensureStageRun falls back to
	// a DB query when it is nil (non-agent stages).
	stageRun, err := o.ensureStageRun(ctx, task, latest)
	if err != nil {
		return nil, fmt.Errorf("orchestrator.ensureStageRun: %w", err)
	}

	// Re-entry guard: if the run is already running with a live PID, return without spawning.
	if handler.RequiresAgent() {
		pid := 0
		if stageRun.Pid != nil {
			pid = *stageRun.Pid
		}
		if (stageRun.Status == "running" || stageRun.Status == "awaiting_user") && IsPidAlive(pid) {
			slog.Info("orchestrator: re-entry skipped — live PID already running",
				"stage", stageRun.Stage, "runID", stageRun.ID, "pid", pid)
			return stageRun, nil
		}
	}

	now := time.Now()
	stageRun, err = o.opts.StageRunRepo.Update(ctx, stageRun.ID, repo.UpdateStageRunInput{
		Status:    strPtr("running"),
		StartedAt: &now,
	})
	if err != nil {
		return nil, fmt.Errorf("orchestrator.updateStageRunRunning: %w", err)
	}

	perms, _ := o.opts.PermissionRepo.ListTaskPermissions(ctx, task.ID)
	allRuns, _ := o.opts.StageRunRepo.ListForTask(ctx, task.ID)
	prevOutput := o.getPreviousStageOutput(ctx, task)
	priorIterOutput := o.getPriorIterationOutput(ctx, task, stageRun)

	var resumeSessionID string
	if opts != nil {
		resumeSessionID = opts.ResumeSessionID
	}
	var userAdditionalPrompt string
	if opts != nil {
		userAdditionalPrompt = opts.UserAdditionalPrompt
	}

	stageCtx := &StageContext{
		Ctx:                  ctx,
		Task:                 task,
		StageRun:             stageRun,
		Permissions:          perms,
		AllStageRuns:         allRuns,
		PreviousOutput:       prevOutput,
		PriorIterationOutput: priorIterOutput,
		ResumeSessionID:      resumeSessionID,
		UserAdditionalPrompt: userAdditionalPrompt,
		MCPToken:             o.opts.MCPToken,
		MCPUrl:               o.opts.MCPUrl,
		SystemPromptRepo:     o.opts.SystemPromptRepo,
		ResolveSpawner:       o.opts.ResolveSpawner,
		DispatchHTTPSpawn: func(stageRunID, taskID string, spawn func() (string, error)) {
			go func() {
				o.httpPoolSem <- struct{}{} // acquire pool slot
				defer func() { <-o.httpPoolSem }()
				sessionFile, err := spawn()
				o.httpResultCh <- httpSpawnResult{
					stageRunID:  stageRunID,
					taskID:      taskID,
					sessionFile: sessionFile,
					err:         err,
				}
			}()
		},
		RecordAudit: func(action string, details map[string]any) {
			_ = o.opts.AuditRepo.RecordTaskAudit(ctx, task.ID, nil, action, "task:"+task.ID, details)
		},
		RequestPermission: func(tool, pattern, reason string) *ent.PermissionRequest {
			pat := (*string)(nil)
			if pattern != "" {
				pat = &pattern
			}
			rsn := (*string)(nil)
			if reason != "" {
				rsn = &reason
			}
			req, err := o.opts.PermissionRepo.CreatePermissionRequest(ctx, repo.CreatePermissionRequestInput{
				StageRunID: stageRun.ID,
				Tool:       tool,
				Pattern:    pat,
				Reason:     rsn,
			})
			if err != nil {
				slog.Error("orchestrator: CreatePermissionRequest failed", "err", err)
				return nil
			}
			if o.opts.OnPermissionRequest != nil {
				o.opts.OnPermissionRequest(task.ID, req)
			}
			return req
		},
	}

	transition, execErr := handler.Execute(stageCtx)
	if execErr != nil {
		transition = FailTransition{Reason: execErr.Error()}
	}

	return o.applyTransition(ctx, task, stageRun, transition)
}

// getTaskMutex returns (or creates) the per-task mutex, ensuring serialized
// ProgressTask calls for the same task ID.
func (o *PipelineOrchestrator) getTaskMutex(taskID string) *sync.Mutex {
	mu, _ := o.taskLocks.LoadOrStore(taskID, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

// sweepApplyTransition applies a transition from within a sweep, guarded by a
// TryLock on the per-task mutex. Returns (nil, false, nil) when the lock is
// already held — progressTask is actively processing the task, so the run is
// not truly an orphan; the sweep defers to the next tick.
func (o *PipelineOrchestrator) sweepApplyTransition(ctx context.Context, task *ent.Task, run *ent.StageRun, t StageTransition) (*ent.StageRun, bool, error) {
	mu := o.getTaskMutex(task.ID)
	if !mu.TryLock() {
		return nil, false, nil
	}
	defer mu.Unlock()
	result, err := o.applyTransition(ctx, task, run, t)
	return result, true, err
}
