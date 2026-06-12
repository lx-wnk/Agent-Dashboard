package pipeline

import (
	"context"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// StageTransition is a sealed interface. Only types in this package satisfy it.
// applyTransition must handle every concrete type; unhandled type panics.
type StageTransition interface{ isTransition() }

type NextTransition struct {
	Stage         string
	MetadataPatch map[string]any
	MetaClear     bool
	Output        map[string]any
}

type DoneTransition struct {
	Output map[string]any
}

type FailTransition struct {
	Reason string
	Output map[string]any
}

type WaitUserTransition struct {
	Reason string
	Output map[string]any
	// AgentDone signals that the agent process has already exited normally
	// (e.g. review cycle limit reached). applyTransition will clear the PID
	// so the dead-PID reaper does not immediately re-fail the run.
	AgentDone bool
}

type IterateTransition struct {
	Output map[string]any
}

type OnHoldTransition struct {
	PermissionRequestID string
	Output              map[string]any
}

type AsyncRunningTransition struct {
	PID         int
	SessionID   string
	SessionFile string // path to synthetic JSONL; empty for Claude (discovered normally)
	Output      map[string]any
}

func (NextTransition) isTransition()         {}
func (DoneTransition) isTransition()         {}
func (FailTransition) isTransition()         {}
func (WaitUserTransition) isTransition()     {}
func (IterateTransition) isTransition()      {}
func (OnHoldTransition) isTransition()       {}
func (AsyncRunningTransition) isTransition() {}

// SystemPromptQuerier is a narrow interface so pipeline does not import the full repo package.
type SystemPromptQuerier interface {
	ListForStage(ctx context.Context, stage string) ([]*ent.SystemPrompt, error)
}

// SpawnerResolverFunc resolves the effective DB spawner row for a task right
// before `exec`. The pipeline only needs the resolved spawner; the actual
// resolver lives in internal/services and is wired by the composition root.
// Errors are propagated and abort the spawn — callers must NEVER silently
// fall back when an explicit reference fails to load.
type SpawnerResolverFunc func(ctx context.Context, taskID string) (*ent.Spawner, error)

// StageContext is passed to stage handlers.
type StageContext struct {
	Ctx                  context.Context
	Task                 *ent.Task
	StageRun             *ent.StageRun
	Permissions          []*ent.TaskPermission
	AllStageRuns         []*ent.StageRun
	PreviousOutput       map[string]any
	PriorIterationOutput map[string]any
	ResumeSessionID      string
	UserAdditionalPrompt string
	MCPToken             string
	MCPUrl               string

	// AdditionalDirs holds the paths of project folders beyond the task cwd
	// that the spawned agent should be able to access. Each path is forwarded
	// to the `claude` CLI as a --add-dir flag. Populated from
	// OrchestratorOptions.ResolveAdditionalDirs; nil means no extra dirs.
	AdditionalDirs []string

	// ResolveSpawner returns the effective DB spawner row for the current task
	// (task → project → claude-default). Stage handlers invoke this right
	// before the native Claude spawn so the resulting ent.Spawner can be
	// passed through SpawnAgentOptions. When nil, the legacy `claude` path
	// is used unchanged.
	ResolveSpawner SpawnerResolverFunc

	// SystemPromptRepo is used to fetch custom system prompt overrides for this stage.
	// May be nil if the feature is not configured.
	SystemPromptRepo SystemPromptQuerier

	// DispatchHTTPSpawn runs the given HTTP spawn function in the goroutine pool
	// and returns immediately. The caller should return AsyncRunningTransition{PID:0}
	// after calling this. Results are drained by the orchestrator on the next tick.
	// When nil (e.g. in tests without a live orchestrator), callers must invoke the
	// spawn function synchronously.
	DispatchHTTPSpawn func(stageRunID, taskID string, spawn func() (string, error))

	RecordAudit       func(action string, details map[string]any)
	RequestPermission func(tool, pattern, reason string) *ent.PermissionRequest
}

// StageHandler is implemented by each pipeline stage.
type StageHandler interface {
	Stage() string
	RequiresAgent() bool
	Execute(ctx *StageContext) (StageTransition, error)
}

// PromptBundle is the system+user prompt pair passed to the Claude CLI.
type PromptBundle struct {
	SystemPrompt string
	UserPrompt   string
}

// StageOrder defines canonical stage progression.
var StageOrder = []string{
	"concept",
	"backlog",
	"implementation",
	"self_review",
	"finalization",
	"done",
}

// NextStage returns the next stage after s, or "done" if s is the last stage.
func NextStage(s string) string {
	for i, stage := range StageOrder {
		if stage == s && i < len(StageOrder)-1 {
			return StageOrder[i+1]
		}
	}
	return "done"
}

// IsTerminalStage returns true for done and cancelled.
func IsTerminalStage(s string) bool {
	return s == "done" || s == "cancelled"
}

// OrchestratorOptions configures the PipelineOrchestrator.
type OrchestratorOptions struct {
	PollInterval     time.Duration
	Client           *ent.Client
	TaskRepo         repo.TaskRepo
	StageRunRepo     repo.StageRunRepo
	PermissionRepo   repo.PermissionRepo
	AuditRepo        repo.AuditEventRepo
	ConfigRepo       repo.PipelineConfigRepo
	SystemPromptRepo SystemPromptQuerier

	// MCPToken and MCPUrl are injected into spawned stage agents via
	// DASHBOARD_MCP_TOKEN / DASHBOARD_MCP_URL so the channel bridge can
	// authenticate back-calls to the dashboard REST API.
	MCPToken string
	MCPUrl   string

	// WorktreeRoot is the base directory for auto-created git worktrees.
	// Defaults to ~/<worktree.DefaultRootDirName> when empty (see worktree.DefaultRoot).
	// Set via DASHBOARD_WORKTREE_ROOT.
	WorktreeRoot string

	// ForceWorktrees ensures every agent-driven task runs in an isolated git worktree,
	// even when task.SourceBranch is not set. The branch is derived as "feat/<slug>".
	// Set via DASHBOARD_FORCE_WORKTREES=true.
	ForceWorktrees bool

	// ResolveSpawner returns the effective DB spawner row for a task right
	// before the native Claude path is taken. When nil, stage handlers spawn
	// with the legacy `claude` CLI (current behaviour).
	ResolveSpawner SpawnerResolverFunc

	// ResolveAdditionalDirs returns the extra directory paths (beyond task.Cwd)
	// that the spawned agent should be able to reach via --add-dir. Called once
	// per task just before StageContext construction. When nil, or when the
	// function returns nil/empty, no extra directories are added. Errors should
	// be logged and treated as an empty result — never block the spawn.
	ResolveAdditionalDirs func(ctx context.Context, task *ent.Task) []string

	OnPermissionRequest func(taskID string, req *ent.PermissionRequest)
	OnStageFailed       func(taskID string, info StageFailedInfo)
	// OnTaskChanged is called after every successful state-machine transition.
	// payload carries the value returned by BuildTaskPayload (if set), which was
	// read inside the same DB transaction as the writes, guaranteeing a consistent
	// view. Callers that invoke OnTaskChanged outside a transaction pass nil;
	// the closure in di_pipeline.go falls back to a live read in that case.
	OnTaskChanged func(taskID string, transitionKind string, payload any)
	// BuildTaskPayload, when non-nil, is called inside applyTransitionWrites (before
	// tx.Commit) with tx-bound repos so that the returned snapshot reflects the
	// just-applied writes. The result is forwarded verbatim as the payload argument
	// to OnTaskChanged after the commit succeeds. Returning nil (e.g. on error)
	// tells OnTaskChanged to fall back to a live read. The pipeline package does not
	// interpret the returned value — callers own its type.
	BuildTaskPayload func(ctx context.Context, taskID string, srRepo repo.StageRunRepo, permRepo repo.PermissionRepo) any
}

// StageFailedInfo carries failure metadata to the OnStageFailed callback.
type StageFailedInfo struct {
	StageRunID string
	Stage      string
	Iteration  int
	Error      string
}
