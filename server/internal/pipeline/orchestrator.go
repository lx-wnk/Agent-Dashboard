package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"syscall"
	"time"

	sdk "github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

const (
	defaultPollInterval        = 2 * time.Second
	maxParallelKey             = "maxParallelOrchestrators"
	defaultMaxParallel         = 3
	stageTimeoutKey            = "stageTimeoutSeconds"
	defaultStageTimeoutSeconds = 1800
	awaitingUserTimeoutKey     = "awaitingUserTimeoutSeconds"
	defaultAwaitingUserTimeout = 14400 // 4h
	pendingStaleDuration = 5 * time.Minute
	maxReviewCyclesKey         = "maxReviewCycles"
	defaultMaxReviewCycles     = 3
)

// httpSpawnResult carries the outcome of an asynchronous HTTP-adapter spawn.
// Written by the goroutine pool worker and drained by tick() on the next cycle.
type httpSpawnResult struct {
	stageRunID  string
	taskID      string
	sessionFile string // path to synthetic session file written by the adapter
	err         error
}

// PipelineOrchestrator drives the task pipeline state machine.
type PipelineOrchestrator struct {
	opts              OrchestratorOptions
	taskLocks         sync.Map // map[taskID string]*sync.Mutex
	handlerOverrides  sync.Map // map[stage string]StageHandler — test seam
	detectCompletion  func(*ent.StageRun, string, CompletionDeps) (CompletionResult, error)
	configCache       sync.Map // map[key string]cachedConfig
	httpResultCh      chan httpSpawnResult // buffered channel for goroutine pool results
	httpPoolSem       chan struct{}        // semaphore: limits concurrent HTTP spawns
}

type cachedConfig struct {
	value     int
	expiresAt time.Time
}

// ProgressOpts carries optional parameters for ProgressTask.
type ProgressOpts struct {
	ResumeSessionID      string
	UserAdditionalPrompt string
}

// NewOrchestrator constructs a PipelineOrchestrator with the given options.
// All repo fields in opts are required (validated at construction).
func NewOrchestrator(opts OrchestratorOptions) (*PipelineOrchestrator, error) {
	if opts.TaskRepo == nil || opts.StageRunRepo == nil || opts.PermissionRepo == nil || opts.AuditRepo == nil || opts.ConfigRepo == nil {
		return nil, fmt.Errorf("NewOrchestrator: all repo fields are required")
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = defaultPollInterval
	}
	poolSize := defaultMaxParallel
	o := &PipelineOrchestrator{
		opts:             opts,
		detectCompletion: DetectCompletion,
		httpPoolSem:      make(chan struct{}, poolSize),
		httpResultCh:     make(chan httpSpawnResult, poolSize*2),
	}
	return o, nil
}

// SetHandlerOverride replaces a stage handler — test seam only.
func (o *PipelineOrchestrator) SetHandlerOverride(stage string, h StageHandler) {
	o.handlerOverrides.Store(stage, h)
}

// ClearHandlerOverrides removes all test handler overrides.
func (o *PipelineOrchestrator) ClearHandlerOverrides() {
	o.handlerOverrides.Range(func(k, _ any) bool { o.handlerOverrides.Delete(k); return true })
}

// SetCompletionDetector replaces the completion detection function — test seam.
func (o *PipelineOrchestrator) SetCompletionDetector(fn func(*ent.StageRun, string, CompletionDeps) (CompletionResult, error)) {
	o.detectCompletion = fn
}

// InvalidateConfigCache clears the TTL config cache — call after REST writes to pipeline_config.
func (o *PipelineOrchestrator) InvalidateConfigCache() {
	o.configCache.Range(func(k, _ any) bool { o.configCache.Delete(k); return true })
}

func (o *PipelineOrchestrator) resolveHandler(stage string) StageHandler {
	if h, ok := o.handlerOverrides.Load(stage); ok {
		return h.(StageHandler)
	}
	return GetHandlerForStage(stage)
}

func (o *PipelineOrchestrator) getCachedConfigNumber(ctx context.Context, key string, fallback int) int {
	if v, ok := o.configCache.Load(key); ok {
		c := v.(cachedConfig)
		if time.Now().Before(c.expiresAt) {
			return c.value
		}
	}
	n := int(o.opts.ConfigRepo.GetNumber(ctx, key, float64(fallback)))
	o.configCache.Store(key, cachedConfig{value: n, expiresAt: time.Now().Add(60 * time.Second)})
	return n
}

// Run starts the orchestrator tick loop. It blocks until ctx is cancelled.
// Must be run in an errgroup goroutine alongside the HTTP server.
func (o *PipelineOrchestrator) Run(ctx context.Context) error {
	o.recoverRunningStageRuns(ctx)
	ticker := time.NewTicker(o.opts.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := o.tick(ctx); err != nil {
				slog.Error("orchestrator tick error", "err", err)
			}
		}
	}
}

// drainHTTPResults processes all pending goroutine-pool results without blocking.
// Successful results write the synthetic session file path into stage_run.output
// so finalizeCompletedAsyncRuns can pick them up on the next tick.
// Failed results mark the stage_run directly as failed.
func (o *PipelineOrchestrator) drainHTTPResults(ctx context.Context) {
	for {
		select {
		case res := <-o.httpResultCh:
			if res.err != nil {
				slog.Error("orchestrator: HTTP spawn goroutine failed", "stageRunID", res.stageRunID, "err", res.err)
				run, getErr := o.opts.StageRunRepo.GetByID(ctx, res.stageRunID)
				if getErr != nil || run == nil || run.Status != "running" {
					continue
				}
				task, taskErr := o.opts.TaskRepo.GetByID(ctx, res.taskID)
				if taskErr != nil {
					continue
				}
				if _, err := o.applyTransition(ctx, task, run, FailTransition{
					Reason: fmt.Sprintf("HTTP adapter error: %s", res.err),
				}); err != nil {
					slog.Error("drainHTTPResults.applyFail", "err", err)
				}
			} else {
				// Write the synthetic session file path into stage_run.output so
				// finalizeCompletedAsyncRuns detects it on the next tick.
				run, getErr := o.opts.StageRunRepo.GetByID(ctx, res.stageRunID)
				if getErr != nil || run == nil {
					continue
				}
				output := map[string]any{"synthetic_session_file": res.sessionFile}
				if run.Output != nil {
					// Merge: preserve any prior output keys (e.g. spawner name logged at spawn time).
					merged := make(map[string]any, len(run.Output)+1)
					for k, v := range run.Output {
						merged[k] = v
					}
					merged["synthetic_session_file"] = res.sessionFile
					output = merged
				}
				if _, err := o.opts.StageRunRepo.Update(ctx, res.stageRunID, repo.UpdateStageRunInput{
					Output: output,
				}); err != nil {
					slog.Error("drainHTTPResults.writeSessionFile", "stageRunID", res.stageRunID, "err", err)
				} else {
					slog.Info("orchestrator: HTTP spawn completed — session file recorded",
						"stageRunID", res.stageRunID, "sessionFile", res.sessionFile)
					if o.opts.OnTaskChanged != nil {
						o.opts.OnTaskChanged(res.taskID, "async_running")
					}
				}
			}
		default:
			return // channel empty — nothing more to drain
		}
	}
}

func (o *PipelineOrchestrator) tick(ctx context.Context) error {
	o.drainHTTPResults(ctx)
	allRunning, err := o.opts.StageRunRepo.ListByStatus(ctx, "running", "awaiting_user", "on_hold")
	if err != nil {
		return fmt.Errorf("orchestrator.tick.listRunning: %w", err)
	}
	if err := o.finalizeCompletedAsyncRuns(ctx, allRunning); err != nil {
		slog.Error("finalizeCompletedAsyncRuns error", "err", err)
	}
	if err := o.sweepAwaitingUserRuns(ctx, allRunning); err != nil {
		slog.Error("sweepAwaitingUserRuns error", "err", err)
	}
	if err := o.sweepOrphanRuns(ctx, allRunning); err != nil {
		slog.Error("sweepOrphanRuns error", "err", err)
	}
	o.pickNextTasksForFreeSlots(ctx, allRunning)
	return nil
}

// ProgressTask advances a task from its current stage.
// Calls are serialized per task via a per-task mutex — concurrent callers
// for the same task queue up rather than racing.
func (o *PipelineOrchestrator) ProgressTask(ctx context.Context, taskID string, opts *ProgressOpts) (*ent.StageRun, error) {
	mu := o.getTaskMutex(taskID)
	mu.Lock()
	defer mu.Unlock()
	return o.runProgressTaskLocked(ctx, taskID, opts)
}

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
		Spawner:              o.opts.Spawner,
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
			_ = o.opts.AuditRepo.Append(ctx, repo.AppendAuditInput{
				TaskID:  task.ID,
				Actor:   "orchestrator",
				Action:  action,
				Details: details,
			})
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

func (o *PipelineOrchestrator) applyTransition(ctx context.Context, task *ent.Task, sr *ent.StageRun, t StageTransition) (result *ent.StageRun, retErr error) {
	// When a Client is available, wrap all DB writes in a single transaction to
	// prevent torn state (e.g. stage_run=done while task.current_stage is still
	// the old stage after a mid-write crash or context cancellation).
	if o.opts.Client != nil {
		tx, err := o.opts.Client.Tx(ctx)
		if err != nil {
			return nil, fmt.Errorf("applyTransition.beginTx: %w", err)
		}
		defer func() {
			if retErr != nil {
				_ = tx.Rollback()
			}
		}()
		txSR := repo.NewStageRunRepo(tx.Client())
		txTask := repo.NewTaskRepo(tx.Client())
		txAudit := repo.NewAuditRepo(tx.Client())
		result, retErr = o.applyTransitionWrites(ctx, task, sr, t, txSR, txTask, txAudit)
		if retErr != nil {
			return nil, retErr
		}
		if retErr = tx.Commit(); retErr != nil {
			return nil, fmt.Errorf("applyTransition.commit: %w", retErr)
		}
		return result, nil
	}
	// No client available (e.g. in tests with mocked repos) — write without tx.
	return o.applyTransitionWrites(ctx, task, sr, t, o.opts.StageRunRepo, o.opts.TaskRepo, o.opts.AuditRepo)
}

func (o *PipelineOrchestrator) applyTransitionWrites(
	ctx context.Context,
	task *ent.Task,
	sr *ent.StageRun,
	t StageTransition,
	srRepo repo.StageRunRepo,
	taskRepo repo.TaskRepo,
	auditRepo repo.AuditRepo,
) (*ent.StageRun, error) {
	now := time.Now()
	var updatedRunID string
	var newRunID string
	var postCommit []func()

	switch tr := t.(type) {
	case NextTransition:
		if _, err := srRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
			Status:  strPtr("done"),
			EndedAt: &now,
			Output:  tr.Output,
		}); err != nil {
			return nil, fmt.Errorf("applyTransition.next.updateRun: %w", err)
		}
		taskUpdate := repo.UpdateTaskInput{CurrentStage: &tr.Stage}
		if tr.MetaClear {
			taskUpdate.MetadataClear = true
		} else if tr.MetadataPatch != nil {
			taskUpdate.Metadata = tr.MetadataPatch
		}
		if _, err := taskRepo.Update(ctx, task.ID, taskUpdate); err != nil {
			return nil, fmt.Errorf("applyTransition.next.updateTask: %w", err)
		}
		_ = auditRepo.Append(ctx, repo.AppendAuditInput{
			TaskID: task.ID, Actor: "orchestrator", Action: "stage_transition",
			Details: map[string]any{"from": task.CurrentStage, "to": tr.Stage},
		})
		updatedRunID = sr.ID

	case DoneTransition:
		if _, err := srRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
			Status: strPtr("done"), EndedAt: &now, Output: tr.Output,
		}); err != nil {
			return nil, fmt.Errorf("applyTransition.done.updateRun: %w", err)
		}
		done := "done"
		if _, err := taskRepo.Update(ctx, task.ID, repo.UpdateTaskInput{CurrentStage: &done}); err != nil {
			return nil, fmt.Errorf("applyTransition.done.updateTask: %w", err)
		}
		_ = auditRepo.Append(ctx, repo.AppendAuditInput{TaskID: task.ID, Actor: "orchestrator", Action: "task_done"})
		updatedRunID = sr.ID
		postCommit = append(postCommit, func() {
			o.handleDependentTasks(ctx, task.ID, "done")
			o.taskLocks.Delete(task.ID)
		})

	case FailTransition:
		output := tr.Output
		if output == nil {
			output = map[string]any{}
		}
		output["error"] = tr.Reason
		if _, err := srRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
			Status: strPtr("failed"), EndedAt: &now, Output: output,
		}); err != nil {
			return nil, fmt.Errorf("applyTransition.fail.updateRun: %w", err)
		}
		_ = auditRepo.Append(ctx, repo.AppendAuditInput{
			TaskID: task.ID, Actor: "orchestrator", Action: "stage_failed",
			Details: map[string]any{"stage": sr.Stage, "iteration": sr.Iteration, "error": tr.Reason},
		})
		updatedRunID = sr.ID
		if o.opts.OnStageFailed != nil {
			info := StageFailedInfo{StageRunID: sr.ID, Stage: sr.Stage, Iteration: sr.Iteration, Error: tr.Reason}
			postCommit = append(postCommit, func() { o.opts.OnStageFailed(task.ID, info) })
		}

	case WaitUserTransition:
		if _, err := srRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
			Status: strPtr("awaiting_user"), Output: tr.Output,
		}); err != nil {
			return nil, fmt.Errorf("applyTransition.waitUser.updateRun: %w", err)
		}
		_ = auditRepo.Append(ctx, repo.AppendAuditInput{
			TaskID: task.ID, Actor: "orchestrator", Action: "awaiting_user",
			Details: map[string]any{"reason": tr.Reason},
		})
		updatedRunID = sr.ID

	case IterateTransition:
		if _, err := srRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
			Status: strPtr("done"), EndedAt: &now, Output: tr.Output,
		}); err != nil {
			return nil, fmt.Errorf("applyTransition.iterate.updateRun: %w", err)
		}
		task2, _ := taskRepo.GetByID(ctx, task.ID)
		maxIter := 20
		if task2 != nil {
			maxIter = task2.MaxIterations
		}
		if sr.Iteration+1 >= maxIter {
			failOutput := tr.Output
			if failOutput == nil {
				failOutput = map[string]any{}
			}
			failOutput["error"] = fmt.Sprintf("iteration limit reached (%d)", maxIter)
			if _, err := srRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
				Status: strPtr("failed"), Output: failOutput,
			}); err != nil {
				return nil, fmt.Errorf("applyTransition.iterate.limitFail: %w", err)
			}
			_ = auditRepo.Append(ctx, repo.AppendAuditInput{
				TaskID: task.ID, Actor: "orchestrator", Action: "iteration_limit_reached",
				Details: map[string]any{"maxIter": maxIter, "lastIteration": sr.Iteration},
			})
			updatedRunID = sr.ID
			if o.opts.OnStageFailed != nil {
				info := StageFailedInfo{StageRunID: sr.ID, Stage: sr.Stage, Iteration: sr.Iteration, Error: fmt.Sprintf("iteration limit reached (%d)", maxIter)}
				postCommit = append(postCommit, func() { o.opts.OnStageFailed(task.ID, info) })
			}
		} else {
			newSR, err := srRepo.Create(ctx, repo.CreateStageRunInput{
				TaskID:      task.ID,
				Stage:       sr.Stage,
				Iteration:   sr.Iteration + 1,
				SessionName: BuildSessionName(task.Slug, sr.Stage, sr.Iteration+1),
			})
			if err != nil {
				return nil, fmt.Errorf("applyTransition.iterate.createRun: %w", err)
			}
			updatedRunID = sr.ID
			newRunID = newSR.ID
		}

	case OnHoldTransition:
		if _, err := srRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
			Status: strPtr("on_hold"), Output: tr.Output,
		}); err != nil {
			return nil, fmt.Errorf("applyTransition.onHold.updateRun: %w", err)
		}
		onHold := "on_hold"
		if _, err := taskRepo.Update(ctx, task.ID, repo.UpdateTaskInput{CurrentStage: &onHold}); err != nil {
			return nil, fmt.Errorf("applyTransition.onHold.updateTask: %w", err)
		}
		_ = auditRepo.Append(ctx, repo.AppendAuditInput{
			TaskID: task.ID, Actor: "orchestrator", Action: "moved_on_hold",
			Details: map[string]any{"permissionRequestId": tr.PermissionRequestID},
		})
		updatedRunID = sr.ID

	case AsyncRunningTransition:
		output := tr.Output
		if tr.SessionFile != "" {
			if output == nil {
				output = map[string]any{}
			} else {
				// shallow-copy to avoid mutating the caller's map
				cp := make(map[string]any, len(output)+1)
				for k, v := range output {
					cp[k] = v
				}
				output = cp
			}
			output["synthetic_session_file"] = tr.SessionFile
		}
		update := repo.UpdateStageRunInput{Status: strPtr("running"), Output: output}
		if tr.PID != 0 {
			update.PID = &tr.PID
		}
		if tr.SessionID != "" {
			update.SessionID = &tr.SessionID
		}
		if _, err := srRepo.Update(ctx, sr.ID, update); err != nil {
			return nil, fmt.Errorf("applyTransition.asyncRunning.updateRun: %w", err)
		}
		_ = auditRepo.Append(ctx, repo.AppendAuditInput{
			TaskID: task.ID, Actor: "orchestrator", Action: "agent_spawned",
			Details: map[string]any{"pid": tr.PID, "stage": sr.Stage},
		})
		updatedRunID = sr.ID

	default:
		panic(fmt.Sprintf("orchestrator.applyTransition: unhandled transition type %T", t))
	}

	// Post-commit side effects (must run after writes are committed).
	for _, fn := range postCommit {
		fn()
	}

	if o.opts.OnTaskChanged != nil {
		kind := transitionKindName(t)
		o.opts.OnTaskChanged(task.ID, kind)
	}

	targetID := updatedRunID
	if newRunID != "" {
		targetID = newRunID
	}
	result, err := o.opts.StageRunRepo.GetByID(ctx, targetID)
	if err != nil {
		return nil, fmt.Errorf("applyTransition.getResult: %w", err)
	}
	return result, nil
}

func transitionKindName(t StageTransition) string {
	switch t.(type) {
	case NextTransition:
		return "next"
	case DoneTransition:
		return "done"
	case FailTransition:
		return "fail"
	case WaitUserTransition:
		return "wait_user"
	case IterateTransition:
		return "iterate"
	case OnHoldTransition:
		return "on_hold"
	case AsyncRunningTransition:
		return "async_running"
	default:
		return "unknown"
	}
}

func strPtr(s string) *string { return &s }

func (o *PipelineOrchestrator) ensureStageRun(ctx context.Context, task *ent.Task, prefetched *ent.StageRun) (*ent.StageRun, error) {
	existing := prefetched
	if existing == nil {
		existing, _ = o.opts.StageRunRepo.GetLatestByTaskAndStage(ctx, task.ID, task.CurrentStage)
	}
	if existing != nil {
		if existing.Status == "pending" || existing.Status == "running" {
			return existing, nil
		}
		// awaiting_user with live PID — don't create a new iteration
		if existing.Status == "awaiting_user" && existing.Pid != nil && IsPidAlive(*existing.Pid) {
			return existing, nil
		}
	}
	iteration := 0
	if existing != nil {
		iteration = existing.Iteration + 1
	}
	return o.opts.StageRunRepo.Create(ctx, repo.CreateStageRunInput{
		TaskID:      task.ID,
		Stage:       task.CurrentStage,
		Iteration:   iteration,
		SessionName: BuildSessionName(task.Slug, task.CurrentStage, iteration),
	})
}

func (o *PipelineOrchestrator) getPreviousStageOutput(ctx context.Context, task *ent.Task) map[string]any {
	for i := len(StageOrder) - 1; i >= 0; i-- {
		if StageOrder[i] == task.CurrentStage {
			for j := i - 1; j >= 0; j-- {
				prev, _ := o.opts.StageRunRepo.GetLatestByTaskAndStage(ctx, task.ID, StageOrder[j])
				if prev != nil && prev.Output != nil {
					return prev.Output
				}
			}
			return nil
		}
	}
	return nil
}

func (o *PipelineOrchestrator) getPriorIterationOutput(ctx context.Context, task *ent.Task, sr *ent.StageRun) map[string]any {
	if sr.Iteration == 0 {
		return nil
	}
	prev, _ := o.opts.StageRunRepo.GetByTaskStageIteration(ctx, task.ID, sr.Stage, sr.Iteration-1)
	if prev != nil {
		return prev.Output
	}
	return nil
}

func (o *PipelineOrchestrator) hasFreeRunnerSlot(ctx context.Context, exceptTaskID string) bool {
	max := o.getCachedConfigNumber(ctx, maxParallelKey, defaultMaxParallel)
	running, _ := o.opts.StageRunRepo.ListByStatus(ctx, "running")
	busyTaskIDs := make(map[string]bool)
	for _, r := range running {
		if r.TaskID != exceptTaskID {
			busyTaskIDs[r.TaskID] = true
		}
	}
	return len(busyTaskIDs) < max
}

func (o *PipelineOrchestrator) pickNextTasksForFreeSlots(ctx context.Context, allRunning []*ent.StageRun) {
	max := o.getCachedConfigNumber(ctx, maxParallelKey, defaultMaxParallel)
	busyTaskIDs := make(map[string]bool)
	for _, r := range allRunning {
		if r.Status == "running" {
			busyTaskIDs[r.TaskID] = true
		}
	}
	freeSlots := max - len(busyTaskIDs)
	if freeSlots <= 0 {
		return
	}
	candidates, _ := o.opts.TaskRepo.ListPickable(ctx)

	// Batch-fetch the latest run per candidate to avoid N+1 per task.
	candidateIDs := make([]string, len(candidates))
	for i, t := range candidates {
		candidateIDs[i] = t.ID
	}
	var latestByTask map[string]*ent.StageRun
	if len(candidateIDs) > 0 {
		latestByTask, _ = o.opts.StageRunRepo.GetLatestForTasks(ctx, candidateIDs)
	}

	var ready []*ent.Task
	for _, t := range candidates {
		if busyTaskIDs[t.ID] {
			continue
		}
		// Only skip if the latest run is specifically on the current stage and blocking.
		// If the latest run is for a different stage, no run exists for currentStage yet.
		if latest := latestByTask[t.ID]; latest != nil && latest.Stage == t.CurrentStage &&
			(latest.Status == "awaiting_user" || latest.Status == "failed") {
			continue
		}
		ready = append(ready, t)
	}
	// Sort: silver_bullet desc → stage index desc → priority desc → created_at asc
	sortPickCandidates(ready)
	picks := ready
	if len(picks) > freeSlots {
		picks = picks[:freeSlots]
	}
	for _, task := range picks {
		if _, err := o.ProgressTask(ctx, task.ID, nil); err != nil {
			slog.Error("orchestrator: pickup failed", "taskID", task.ID, "err", err)
		}
	}
}

func sortPickCandidates(tasks []*ent.Task) {
	// Insertion sort (small N, simple)
	priorityRank := map[string]int{"high": 3, "medium": 2, "low": 1}
	stageIdx := func(s string) int {
		for i, st := range StageOrder {
			if st == s {
				return i
			}
		}
		return -1
	}
	for i := 1; i < len(tasks); i++ {
		for j := i; j > 0; j-- {
			a, b := tasks[j-1], tasks[j]
			if shouldSwap(a, b, priorityRank, stageIdx) {
				tasks[j-1], tasks[j] = tasks[j], tasks[j-1]
			} else {
				break
			}
		}
	}
}

func shouldSwap(a, b *ent.Task, priorityRank map[string]int, stageIdx func(string) int) bool {
	if a.SilverBullet != b.SilverBullet {
		return !a.SilverBullet // b has silver bullet — b should come first
	}
	si, sj := stageIdx(a.CurrentStage), stageIdx(b.CurrentStage)
	if si != sj {
		return si < sj // b is further along — b should come first
	}
	pi, pj := priorityRank[a.Priority], priorityRank[b.Priority]
	if pi != pj {
		return pi < pj // b has higher priority — b should come first
	}
	return a.CreatedAt.After(b.CreatedAt) // b is older — b should come first
}

func (o *PipelineOrchestrator) sweepAwaitingUserRuns(ctx context.Context, allRunning []*ent.StageRun) error {
	timeoutSec := o.getCachedConfigNumber(ctx, awaitingUserTimeoutKey, defaultAwaitingUserTimeout)
	for _, run := range allRunning {
		if run.Status != "awaiting_user" {
			continue
		}
		task, err := o.opts.TaskRepo.GetByID(ctx, run.TaskID)
		if err != nil {
			continue
		}
		// Only reap runs that had a live agent process which has since died.
		// Nil-PID runs are legitimate: concept/backlog WaitUser transitions never
		// spawn an agent, so IsPidAlive(0) == false must not trigger the reaper.
		if run.Pid != nil && !IsPidAlive(*run.Pid) {
			slog.Warn("orchestrator: awaiting_user run has dead PID — reaping as failed",
				"runID", run.ID, "stage", run.Stage, "pid", *run.Pid)
			if _, locked, err := o.sweepApplyTransition(ctx, task, run, FailTransition{
				Reason: "awaiting_user reaper: stage agent exited while permissions pending",
			}); err != nil {
				slog.Error("sweepAwaitingUserRuns.applyTransition", "err", err)
			} else if !locked {
				slog.Debug("sweepAwaitingUserRuns: task locked by progressTask — deferring to next tick", "taskID", task.ID)
			}
			continue
		}
		if timeoutSec > 0 {
			anchor := run.StartedAt
			if run.LastGrantAt != nil {
				anchor = run.LastGrantAt
			}
			if anchor != nil && time.Since(*anchor) > time.Duration(timeoutSec)*time.Second {
				timeoutPid := 0
				if run.Pid != nil {
					timeoutPid = *run.Pid
				}
				slog.Warn("orchestrator: awaiting_user run exceeded wallclock timeout — killing agent",
					"runID", run.ID, "pid", timeoutPid, "timeoutSec", timeoutSec)
				_ = syscallKill(timeoutPid)
				fresh, _ := o.opts.StageRunRepo.GetByID(ctx, run.ID)
				if fresh != nil && fresh.Status == "awaiting_user" {
					elapsed := time.Since(*anchor).Seconds()
					if _, locked, err := o.sweepApplyTransition(ctx, task, fresh, FailTransition{
						Reason: fmt.Sprintf("awaiting_user timeout: ran %.0fs (limit %ds) — agent likely busy-waiting", elapsed, timeoutSec),
					}); err != nil {
						slog.Error("sweepAwaitingUserRuns.timeout.applyTransition", "err", err)
					} else if !locked {
						slog.Debug("sweepAwaitingUserRuns timeout: task locked by progressTask — deferring to next tick", "taskID", task.ID)
					}
				}
			}
		}
	}
	return nil
}

func (o *PipelineOrchestrator) sweepOrphanRuns(ctx context.Context, allRunning []*ent.StageRun) error {
	pendings, _ := o.opts.StageRunRepo.ListPending(ctx)
	all := append(allRunning, pendings...)
	for _, run := range all {
		task, err := o.opts.TaskRepo.GetByID(ctx, run.TaskID)
		if err != nil {
			continue
		}
		// Case 1: task is parked but stage_run is non-terminal
		if task.CurrentStage == "done" || task.CurrentStage == "cancelled" || task.CurrentStage == "on_hold" {
			fresh, _ := o.opts.StageRunRepo.GetByID(ctx, run.ID)
			if fresh == nil || fresh.Status == "done" || fresh.Status == "failed" {
				continue
			}
			pid := 0
			if fresh.Pid != nil {
				pid = *fresh.Pid
			}
			if IsPidAlive(pid) {
				_ = syscallKill(pid)
			}
			slog.Warn("orchestrator: orphan stage_run — task is parked, reaping run as failed",
				"runID", fresh.ID, "taskStage", task.CurrentStage)
			if _, locked, err := o.sweepApplyTransition(ctx, task, fresh, FailTransition{
				Reason: fmt.Sprintf("orphan reaper: task reached %s with stage_run still %s", task.CurrentStage, fresh.Status),
			}); err != nil {
				slog.Error("sweepOrphanRuns.case1.applyTransition", "err", err)
			} else if !locked {
				slog.Debug("sweepOrphanRuns case1: task locked by progressTask — deferring to next tick", "taskID", task.ID)
			}
			continue
		}
		// Case 2: on_hold with dead PID
		if run.Status == "on_hold" && run.Pid != nil && !IsPidAlive(*run.Pid) {
			fresh, _ := o.opts.StageRunRepo.GetByID(ctx, run.ID)
			if fresh == nil || fresh.Status == "done" || fresh.Status == "failed" {
				continue
			}
			slog.Warn("orchestrator: on_hold run has dead PID — reaping as failed", "runID", fresh.ID)
			if _, locked, err := o.sweepApplyTransition(ctx, task, fresh, FailTransition{Reason: "orphan reaper: on_hold agent exited"}); err != nil {
				slog.Error("sweepOrphanRuns.case2.applyTransition", "err", err)
			} else if !locked {
				slog.Debug("sweepOrphanRuns case2: task locked by progressTask — deferring to next tick", "taskID", task.ID)
			}
			continue
		}
		// Case 3: pending stuck > 5 min without a PID
		if run.Status == "pending" && run.Pid == nil && run.StartedAt != nil {
			if time.Since(*run.StartedAt) > pendingStaleDuration {
				fresh, _ := o.opts.StageRunRepo.GetByID(ctx, run.ID)
				if fresh == nil || fresh.Status != "pending" {
					continue
				}
				elapsed := time.Since(*run.StartedAt).Seconds()
				slog.Warn("orchestrator: pending run stuck without spawn — reaping as failed",
					"runID", fresh.ID, "elapsedSec", elapsed)
				if _, locked, err := o.sweepApplyTransition(ctx, task, fresh, FailTransition{
					Reason: fmt.Sprintf("orphan reaper: pending stage_run never promoted to running (%.0fs elapsed)", elapsed),
				}); err != nil {
					slog.Error("sweepOrphanRuns.case3.applyTransition", "err", err)
				} else if !locked {
					slog.Debug("sweepOrphanRuns case3: task locked by progressTask — deferring to next tick", "taskID", task.ID)
				}
			}
		}
	}
	return nil
}

// hasSyntheticSessionFile returns true when the stage_run's output already
// contains the synthetic_session_file key written by drainHTTPResults.
func hasSyntheticSessionFile(run *ent.StageRun) bool {
	if run.Output == nil {
		return false
	}
	sf, ok := run.Output["synthetic_session_file"].(string)
	return ok && sf != ""
}

func (o *PipelineOrchestrator) finalizeCompletedAsyncRuns(ctx context.Context, allRunning []*ent.StageRun) error {
	for _, run := range allRunning {
		if run.Status != "running" {
			continue
		}

		// Determine whether this run belongs to a subprocess-based agent (pid != nil)
		// or an HTTP adapter (pid == nil).
		//
		// HTTP adapter runs (pid IS NULL):
		//   - Still in flight: no synthetic_session_file in output yet → skip.
		//   - Completed:       synthetic_session_file present → fall through to detectCompletion.
		//
		// Subprocess runs (pid != nil):
		//   - Still alive → skip.
		//   - Exited      → fall through to detectCompletion.
		if run.Pid == nil {
			if !hasSyntheticSessionFile(run) {
				// HTTP goroutine has not finished yet — skip until drainHTTPResults writes the file.
				continue
			}
			// Synthetic session file written — run is ready to finalize.
		}
		// Subprocess runs (pid != nil): fall through whether alive or exited.
		// detectCompletion returns "still_running" for live PIDs; cost-budget and
		// stage-timeout enforcement in that branch applies unconditionally.

		task, err := o.opts.TaskRepo.GetByID(ctx, run.TaskID)
		if err != nil {
			continue
		}
		if IsTerminalStage(task.CurrentStage) {
			if run.Pid != nil && IsPidAlive(*run.Pid) {
				_ = syscallKill(*run.Pid)
			}
			if _, err := o.applyTransition(ctx, task, run, FailTransition{Reason: "task cancelled externally"}); err != nil {
				slog.Error("finalizeCompletedAsyncRuns.externalCancel", "err", err)
			}
			continue
		}
		cwd := task.Cwd
		if task.WorktreePath != nil && *task.WorktreePath != "" {
			cwd = *task.WorktreePath
		}

		result, err := o.detectCompletion(run, cwd, CompletionDeps{})
		if err != nil {
			slog.Error("orchestrator: completion detection failed", "runID", run.ID, "err", err)
			continue
		}
		if result.Kind == "still_running" {
			// Try to attach session_id for live cross-link banner (subprocess runs only)
			if run.Pid != nil && run.SessionID == nil && run.StartedAt != nil {
				go o.tryAttachSessionID(ctx, run.ID, task.ID, cwd, *run.StartedAt)
			}
			// Cost budget enforcement (subprocess runs only — HTTP runs finalize atomically)
			if run.Pid != nil && task.CostBudgetCents != nil && *task.CostBudgetCents > 0 {
				spent, _ := o.opts.StageRunRepo.SumCompletedCostCents(ctx, task.ID)
				if spent > int64(*task.CostBudgetCents) {
					slog.Warn("orchestrator: task exceeded cost budget — killing agent",
						"taskID", task.ID, "spent", spent, "budget", *task.CostBudgetCents)
					_ = syscallKill(*run.Pid)
					fresh, _ := o.opts.StageRunRepo.GetByID(ctx, run.ID)
					if fresh != nil && fresh.Status == "running" {
						if _, err := o.applyTransition(ctx, task, fresh, FailTransition{
							Reason: fmt.Sprintf("cost budget exceeded: %d cents spent, limit %d cents", spent, *task.CostBudgetCents),
						}); err != nil {
							slog.Error("finalizeCompletedAsyncRuns.costBudget", "err", err)
						}
					}
					continue
				}
			}
			// Stage timeout enforcement (subprocess runs only)
			if run.Pid != nil {
				timeoutSec := o.getCachedConfigNumber(ctx, stageTimeoutKey, defaultStageTimeoutSeconds)
				if timeoutSec > 0 && run.StartedAt != nil && time.Since(*run.StartedAt) > time.Duration(timeoutSec)*time.Second {
					elapsed := time.Since(*run.StartedAt).Seconds()
					slog.Warn("orchestrator: stage timed out — killing agent",
						"runID", run.ID, "stage", run.Stage, "elapsedSec", elapsed)
					_ = syscallKill(*run.Pid)
					fresh, _ := o.opts.StageRunRepo.GetByID(ctx, run.ID)
					if fresh != nil && fresh.Status == "running" {
						if _, err := o.applyTransition(ctx, task, fresh, FailTransition{
							Reason: fmt.Sprintf("stage timeout: ran %.0fs (limit %ds)", elapsed, timeoutSec),
						}); err != nil {
							slog.Error("finalizeCompletedAsyncRuns.timeout", "err", err)
						}
					}
				}
			}
			continue
		}

		// Process exited (or HTTP adapter finished) — persist token usage for subprocess runs.
		if run.Pid != nil && run.SessionID != nil {
			go o.updateTokenUsage(ctx, run.ID, cwd, *run.SessionID)
		}

		fresh, _ := o.opts.StageRunRepo.GetByID(ctx, run.ID)
		if fresh == nil || fresh.Status != "running" {
			continue
		}

		if result.Kind == "completed" {
			transition := o.decideCompletedTransition(ctx, task, fresh, result.Output)
			if _, err := o.applyTransition(ctx, task, fresh, transition); err != nil {
				slog.Error("finalizeCompletedAsyncRuns.applyTransition.completed", "err", err)
			}
			continue
		}

		// failed
		if !result.Retryable {
			if _, err := o.applyTransition(ctx, task, fresh, FailTransition{Reason: result.Error, Output: result.Output}); err != nil {
				slog.Error("finalizeCompletedAsyncRuns.applyTransition.hardFail", "err", err)
			}
			continue
		}
		// Schema rejection retry logic: iter 0 → iterate; iter >= 1 → wait_user
		if fresh.Iteration == 0 {
			if _, err := o.applyTransition(ctx, task, fresh, IterateTransition{Output: map[string]any{
				"validation_error": result.Error,
				"rejected_output":  result.Output,
			}}); err != nil {
				slog.Error("finalizeCompletedAsyncRuns.applyTransition.iterate", "err", err)
			}
		} else {
			if _, err := o.applyTransition(ctx, task, fresh, WaitUserTransition{
				Reason: fmt.Sprintf("schema validation failed twice at stage %s: %s", fresh.Stage, result.Error),
				Output: map[string]any{"validation_error": result.Error, "rejected_output": result.Output},
			}); err != nil {
				slog.Error("finalizeCompletedAsyncRuns.applyTransition.waitUser", "err", err)
			}
		}
	}
	return nil
}

func (o *PipelineOrchestrator) decideCompletedTransition(ctx context.Context, task *ent.Task, run *ent.StageRun, output map[string]any) StageTransition {
	if run.Stage == "finalization" {
		return DoneTransition{Output: output}
	}
	if run.Stage == "self_review" {
		passed, _ := output["passed"].(bool)
		if !passed {
			feedback := SummarizeReviewFindings(output)
			prevCycles := 0
			if task.Metadata != nil {
				if v, ok := task.Metadata["review_cycles"].(float64); ok {
					prevCycles = int(v)
				}
			}
			cycles := prevCycles + 1
			maxCycles := o.getCachedConfigNumber(ctx, maxReviewCyclesKey, defaultMaxReviewCycles)
			if task.Metadata != nil {
				if v, ok := task.Metadata["maxReviewCycles"].(float64); ok && int(v) > 0 {
					maxCycles = int(v)
				}
			}
			if cycles >= maxCycles {
				return WaitUserTransition{
					Reason: fmt.Sprintf("review cycle limit (%d) reached", maxCycles),
					Output: output,
				}
			}
			meta := map[string]any{}
			if task.Metadata != nil {
				for k, v := range task.Metadata {
					meta[k] = v
				}
			}
			meta["review_feedback"] = feedback
			meta["review_cycles"] = cycles
			return NextTransition{Stage: "implementation", Output: output, MetadataPatch: meta}
		}
		// Passed — clear stale review feedback
		if task.Metadata != nil {
			if _, hasFeedback := task.Metadata["review_feedback"]; hasFeedback {
				rest := map[string]any{}
				for k, v := range task.Metadata {
					if k != "review_feedback" && k != "review_cycles" {
						rest[k] = v
					}
				}
				if len(rest) == 0 {
					return NextTransition{Stage: "finalization", Output: output, MetaClear: true}
				}
				return NextTransition{Stage: "finalization", Output: output, MetadataPatch: rest}
			}
		}
		return NextTransition{Stage: "finalization", Output: output}
	}
	return NextTransition{Stage: NextStage(run.Stage), Output: output}
}

func (o *PipelineOrchestrator) recoverRunningStageRuns(ctx context.Context) {
	running, _ := o.opts.StageRunRepo.ListByStatus(ctx, "running")
	for _, run := range running {
		decision := DecideRecovery(run)
		_ = o.opts.AuditRepo.Append(ctx, repo.AppendAuditInput{
			TaskID: run.TaskID, Actor: "system", Action: "recovery_decision",
			Details: map[string]any{"stage": run.Stage, "iteration": run.Iteration, "decision": decision.Kind, "reason": decision.Reason},
		})
		if decision.Kind == "alive" {
			continue
		}
		if decision.Kind == "resume" {
			_, _ = o.opts.StageRunRepo.Update(ctx, run.ID, repo.UpdateStageRunInput{Status: strPtr("pending"), PIDClear: true})
		} else {
			now := time.Now()
			_, _ = o.opts.StageRunRepo.Update(ctx, run.ID, repo.UpdateStageRunInput{
				Status:  strPtr("failed"),
				EndedAt: &now,
				Output:  map[string]any{"error": "orchestrator crashed before completion; no session to resume"},
			})
		}
	}
}

func (o *PipelineOrchestrator) handleDependentTasks(ctx context.Context, taskID, newStage string) {
	// Dependency cascade — Phase 3 stub: no task_dependency traversal yet.
	// Full cascade (cancel, on_hold, start actions) is part of the dependency
	// sub-feature; implement when TaskDependency repo methods are available.
	if o.opts.OnTaskChanged != nil {
		o.opts.OnTaskChanged(taskID, "dependent_check")
	}
}

func (o *PipelineOrchestrator) tryAttachSessionID(ctx context.Context, stageRunID, taskID, cwd string, startedAt time.Time) {
	sid, err := FindNewestSessionID(cwd, startedAt.Format("2006-01-02T15:04:05Z"))
	if err != nil || sid == "" {
		return
	}
	if err := AttachSessionID(ctx, stageRunID, sid, o.opts.StageRunRepo); err != nil {
		slog.Warn("orchestrator.tryAttachSessionID", "err", err)
		return
	}
	if o.opts.OnTaskChanged != nil {
		o.opts.OnTaskChanged(taskID, "async_running")
	}
}

func (o *PipelineOrchestrator) updateTokenUsage(ctx context.Context, stageRunID, cwd, sessionID string) {
	summary, err := ReadSessionTokenSummary(cwd, sessionID)
	if err != nil {
		return
	}
	total := summary.InputTokens + summary.OutputTokens + summary.CacheCreationTokens + summary.CacheReadTokens
	if total == 0 {
		return
	}
	costUsd := parser.EstimateCost(sdk.TokenUsage{
		InputTokens:         summary.InputTokens,
		OutputTokens:        summary.OutputTokens,
		CacheCreationTokens: summary.CacheCreationTokens,
		CacheReadTokens:     summary.CacheReadTokens,
	}, summary.Model)
	costCents := int(costUsd * 100)
	_, _ = o.opts.StageRunRepo.Update(ctx, stageRunID, repo.UpdateStageRunInput{
		TokensUsed: &total,
		CostCents:  &costCents,
	})
}

// NotifyTaskTerminated is called by cancel routes to cascade terminal state to dependents.
// It also removes the per-task mutex so taskLocks does not grow unbounded.
func (o *PipelineOrchestrator) NotifyTaskTerminated(ctx context.Context, taskID, stage string) {
	o.taskLocks.Delete(taskID)
	o.handleDependentTasks(ctx, taskID, stage)
}

// ResumeFromUser re-runs progressTask after a user action (permission grant, retry).
func (o *PipelineOrchestrator) ResumeFromUser(ctx context.Context, taskID string) (*ent.StageRun, error) {
	return o.ProgressTask(ctx, taskID, nil)
}

func syscallKill(pid int) error {
	if pid <= 0 {
		return nil
	}
	// Signal the process group so spawned subprocesses (Setpgid: true) are also terminated.
	if err := syscall.Kill(-pid, syscall.SIGTERM); err == nil {
		return nil
	}
	// Fall back to signaling the process directly if group kill fails (e.g. process already exited).
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGTERM)
}
