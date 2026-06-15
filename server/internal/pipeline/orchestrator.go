// Package pipeline implements the task pipeline state machine.
//
// File layout (all in package pipeline):
//
//	orchestrator.go     — PipelineOrchestrator struct, Run/tick entry, helpers
//	runner_picker.go    — pickNextTasksForFreeSlots, sort helpers (F-PERF-007, F-PERF-010)
//	sweeps.go           — sweepAwaitingUserRuns, sweepOrphanRuns
//	progress_guards.go  — runProgressTaskLocked (Re-entry Guard + Lingering-Pending Gate)
//	transitions.go      — applyTransition, applyTransitionWrites, decideCompletedTransition
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
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pricing"
)

const (
	defaultPollInterval        = 2 * time.Second
	maxParallelKey             = "maxParallelOrchestrators"
	defaultMaxParallel         = 3
	stageTimeoutKey            = "stageTimeoutSeconds"
	defaultStageTimeoutSeconds = db.DefaultStageTimeoutSeconds
	awaitingUserTimeoutKey     = "awaitingUserTimeoutSeconds"
	defaultAwaitingUserTimeout = 14400 // 4h
	pendingStaleDuration       = 5 * time.Minute
	maxReviewCyclesKey         = "maxReviewCycles"
	defaultMaxReviewCycles     = 3
	maxAutoRetriesKey          = "maxAutoRetries"
	defaultMaxAutoRetries      = 3
	retryBackoffKey            = "retryBackoffSeconds"
	defaultRetryBackoff        = 60
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
	opts             OrchestratorOptions
	taskLocks        sync.Map // map[taskID string]*sync.Mutex
	handlerOverrides sync.Map // map[stage string]StageHandler — test seam
	detectCompletion func(*ent.StageRun, string, CompletionDeps) (CompletionResult, error)
	configCache      sync.Map // map[key string]cachedConfig

	httpResultCh chan httpSpawnResult // buffered channel for goroutine pool results
	httpPoolSem  chan struct{}        // semaphore: limits concurrent HTTP spawns

	// F-PERF-008: dedupe inflight tryAttachSessionID goroutines.
	// Key: stageRunID string; Value: struct{} (presence = goroutine in flight).
	attachInFlight sync.Map
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
						o.opts.OnTaskChanged(res.taskID, "async_running", nil)
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
	if err := o.sweepRequeueableRuns(ctx); err != nil {
		slog.Error("sweepRequeueableRuns error", "err", err)
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

// hasFreeRunnerSlot checks whether a runner slot is available for the given
// task. Called from the route/MCP-triggered ProgressTask path where no
// prefetched running set is available — issues one ListByStatus("running") query.
// F-PERF-007: the tick()-driven path uses pickNextTasksForFreeSlots which
// derives busyTaskIDs from the already-loaded allRunning slice, bypassing this
// function entirely for the normal picker flow.
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
			// Try to attach session_id for live cross-link banner (subprocess runs only).
			// F-PERF-008: dedupe inflight goroutines via attachInFlight sync.Map.
			if run.Pid != nil && run.SessionID == nil && run.StartedAt != nil {
				if _, loaded := o.attachInFlight.LoadOrStore(run.ID, struct{}{}); !loaded {
					go func(runID, taskID, cwd string, startedAt time.Time) {
						defer o.attachInFlight.Delete(runID)
						o.tryAttachSessionID(ctx, runID, taskID, cwd, startedAt)
					}(run.ID, task.ID, cwd, *run.StartedAt)
				}
			}
			// Cost budget enforcement (subprocess runs only — HTTP runs finalize atomically)
			if run.Pid != nil && task.CostBudgetCents != nil && *task.CostBudgetCents > 0 {
				spent, err := o.opts.StageRunRepo.SumCompletedCostCents(ctx, task.ID)
				if err != nil {
					slog.Warn("orchestrator: cost budget sum failed — skipping enforcement this tick",
						"taskID", task.ID, "err", err)
				}
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
			// Token budget enforcement (subprocess runs only — HTTP runs finalize atomically)
			if run.Pid != nil && task.TokenBudget != nil && *task.TokenBudget > 0 {
				spent, err := o.opts.StageRunRepo.SumCompletedTokens(ctx, task.ID)
				if err != nil {
					slog.Warn("orchestrator: token budget sum failed — skipping enforcement this tick",
						"taskID", task.ID, "err", err)
				}
				if spent > int64(*task.TokenBudget) {
					slog.Warn("orchestrator: task exceeded token budget — killing agent",
						"taskID", task.ID, "spent", spent, "budget", *task.TokenBudget)
					_ = syscallKill(*run.Pid)
					fresh, _ := o.opts.StageRunRepo.GetByID(ctx, run.ID)
					if fresh != nil && fresh.Status == "running" {
						if _, err := o.applyTransition(ctx, task, fresh, FailTransition{
							Reason: fmt.Sprintf("token budget exceeded: %d tokens used, limit %d tokens", spent, *task.TokenBudget),
						}); err != nil {
							slog.Error("finalizeCompletedAsyncRuns.tokenBudget", "err", err)
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

		// failed — three cases in priority order

		if result.Infra {
			maxRetries := o.getCachedConfigNumber(ctx, maxAutoRetriesKey, defaultMaxAutoRetries)
			if fresh.RetryCount < maxRetries {
				attempt := fresh.RetryCount + 1
				backoffSec := o.getCachedConfigNumber(ctx, retryBackoffKey, defaultRetryBackoff) * attempt // linear backoff
				nextRetryAt := time.Now().Add(time.Duration(backoffSec) * time.Second)
				slog.Info("orchestrator: requeuing infra-failed run",
					"runID", fresh.ID, "stage", fresh.Stage, "attempt", attempt,
					"maxAutoRetries", maxRetries, "backoffSec", backoffSec)
				if _, err := o.applyTransition(ctx, task, fresh, RequeueTransition{
					Reason:      result.Error,
					Output:      result.Output,
					Attempt:     attempt,
					NextRetryAt: nextRetryAt,
				}); err != nil {
					slog.Error("finalizeCompletedAsyncRuns.applyTransition.requeue", "err", err)
				}
			} else {
				slog.Warn("orchestrator: auto-retry budget exhausted — hard failing",
					"runID", fresh.ID, "stage", fresh.Stage, "maxAutoRetries", maxRetries)
				output := make(map[string]any, len(result.Output)+1)
				for k, v := range result.Output {
					output[k] = v
				}
				output["auto_retries_exhausted"] = maxRetries
				if _, err := o.applyTransition(ctx, task, fresh, FailTransition{Reason: result.Error, Output: output}); err != nil {
					slog.Error("finalizeCompletedAsyncRuns.applyTransition.hardFail", "err", err)
				}
			}
			continue
		}

		if result.Retryable {
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
					Reason:    fmt.Sprintf("schema validation failed twice at stage %s: %s", fresh.Stage, result.Error),
					Output:    map[string]any{"validation_error": result.Error, "rejected_output": result.Output},
					AgentDone: true,
				}); err != nil {
					slog.Error("finalizeCompletedAsyncRuns.applyTransition.waitUser", "err", err)
				}
			}
			continue
		}

		if _, err := o.applyTransition(ctx, task, fresh, FailTransition{Reason: result.Error, Output: result.Output}); err != nil {
			slog.Error("finalizeCompletedAsyncRuns.applyTransition.hardFail", "err", err)
		}
	}
	return nil
}

func (o *PipelineOrchestrator) recoverRunningStageRuns(ctx context.Context) {
	running, _ := o.opts.StageRunRepo.ListByStatus(ctx, "running")
	for _, run := range running {
		decision := DecideRecovery(run)
		_ = o.opts.AuditRepo.RecordTaskAudit(ctx, run.TaskID, nil, "recovery_decision", "task:"+run.TaskID,
			map[string]any{"stage": run.Stage, "iteration": run.Iteration, "decision": decision.Kind, "reason": decision.Reason})
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
		o.opts.OnTaskChanged(taskID, "dependent_check", nil)
	}
}

// tryAttachSessionID finds and records the session ID for a live subprocess run.
// Called from a goroutine; deduplication is handled by attachInFlight in the caller.
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
		o.opts.OnTaskChanged(taskID, "async_running", nil)
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
	costUsd := pricing.EstimateCost(sdk.TokenUsage{
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
	// Permission grants reach a running agent only via kill-and-restart:
	// ensureStageRun refuses to spawn a new iteration while the awaiting_user
	// agent's PID is alive, so the grant would never take effect. Reap that
	// paused agent (kill + mark failed) first so the resumed run spawns with the
	// freshly granted permissions applied.
	o.reapAwaitingUserAgent(ctx, taskID)
	return o.ProgressTask(ctx, taskID, nil)
}

// reapAwaitingUserAgent kills the task's current awaiting_user stage-run agent
// and marks the run failed, so a subsequent ProgressTask spawns a fresh
// iteration instead of short-circuiting on the still-alive PID.
func (o *PipelineOrchestrator) reapAwaitingUserAgent(ctx context.Context, taskID string) {
	mu := o.getTaskMutex(taskID)
	mu.Lock()
	defer mu.Unlock()
	task, err := o.opts.TaskRepo.GetByID(ctx, taskID)
	if err != nil || task == nil {
		return
	}
	run, err := o.opts.StageRunRepo.GetLatestByTaskAndStage(ctx, taskID, task.CurrentStage)
	if err != nil || run == nil || run.Status != "awaiting_user" {
		return
	}
	if run.Pid != nil {
		_ = syscallKill(*run.Pid)
	}
	if _, err := o.applyTransition(ctx, task, run, FailTransition{
		Reason: "user resolved permissions — restarting stage with grants applied",
	}); err != nil {
		slog.Error("reapAwaitingUserAgent.applyTransition", "taskID", taskID, "err", err)
	}
}

func strPtr(s string) *string { return &s }

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
