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
}

type IterateTransition struct {
	Output map[string]any
}

type OnHoldTransition struct {
	PermissionRequestID string
	Output              map[string]any
}

type AsyncRunningTransition struct {
	PID       int
	SessionID string
	Output    map[string]any
}

func (NextTransition) isTransition()         {}
func (DoneTransition) isTransition()         {}
func (FailTransition) isTransition()         {}
func (WaitUserTransition) isTransition()     {}
func (IterateTransition) isTransition()      {}
func (OnHoldTransition) isTransition()       {}
func (AsyncRunningTransition) isTransition() {}

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
	PollInterval   time.Duration
	Client         *ent.Client
	TaskRepo       repo.TaskRepo
	StageRunRepo   repo.StageRunRepo
	PermissionRepo repo.PermissionRepo
	AuditRepo      repo.AuditRepo
	ConfigRepo     repo.PipelineConfigRepo

	// MCPToken and MCPUrl are injected into spawned stage agents via
	// DASHBOARD_MCP_TOKEN / DASHBOARD_MCP_URL so the channel bridge can
	// authenticate back-calls to the dashboard REST API.
	MCPToken string
	MCPUrl   string

	OnPermissionRequest func(taskID string, req *ent.PermissionRequest)
	OnStageFailed       func(taskID string, info StageFailedInfo)
	OnTaskChanged       func(taskID string, transitionKind string)
}

// StageFailedInfo carries failure metadata to the OnStageFailed callback.
type StageFailedInfo struct {
	StageRunID string
	Stage      string
	Iteration  int
	Error      string
}
