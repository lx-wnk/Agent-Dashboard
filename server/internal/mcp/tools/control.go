package tools

import (
	"context"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

// ControlOrchestrator is a minimal interface for the orchestrator operations needed by control tools.
// Defined here to avoid a direct runtime import of the pipeline orchestrator from mcp/tools.
type ControlOrchestrator interface {
	ProgressTask(ctx context.Context, taskID string, opts *pipeline.ProgressOpts) (*ent.StageRun, error)
	ResumeFromUser(ctx context.Context, taskID string) (*ent.StageRun, error)
	NotifyTaskTerminated(ctx context.Context, taskID, stage string)
}

// ControlDeps holds dependencies required by the pipeline control tools.
type ControlDeps struct {
	TaskRepo     repo.TaskRepo
	SRRepo       repo.StageRunRepo
	PermRepo     repo.PermissionRepo
	AuditRepo    repo.AuditRepo
	Orchestrator ControlOrchestrator // may be nil in tests
	Broadcast    func(taskID string)
}

// RegisterControlTools registers all 5 control tools into the given registry.
func RegisterControlTools(registry mcp.ToolRegistry, d ControlDeps) {
	registerProgressTask(registry, d)
	registerCancelTask(registry, d)
	registerRetryTask(registry, d)
	registerGrantPermission(registry, d)
	registerResolvePermissionRequest(registry, d)
}

func registerProgressTask(registry mcp.ToolRegistry, d ControlDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "progress_task",
		Description: "Advance a task to the next stage or trigger its current stage handler.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Task ID"},
			},
			"required": []string{"id"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			id, err := mcp.StringArg(args, "id")
			if err != nil {
				return nil, err
			}
			task, err := d.TaskRepo.GetByID(ctx, id)
			if err != nil {
				return nil, mcp.Fail("Task not found: " + id)
			}

			if d.Orchestrator == nil {
				return nil, mcp.Fail("orchestrator not available")
			}
			stageRun, err := d.Orchestrator.ProgressTask(ctx, id, nil)
			if err != nil {
				return nil, mcp.Fail("progress_task: " + err.Error())
			}
			if stageRun == nil {
				return nil, mcp.Fail("Task cannot progress (terminal, not found, or no free runner slot)")
			}
			safeBroadcast(d.Broadcast, id)
			// Refresh task after progression.
			task, _ = d.TaskRepo.GetByID(ctx, id)
			return mcp.OK(map[string]any{"task": task, "stageRun": stageRun})
		},
	})
}

func registerCancelTask(registry mcp.ToolRegistry, d ControlDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "cancel_task",
		Description: "Cancel a task, preventing further stage progression.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Task ID"},
			},
			"required": []string{"id"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			id, err := mcp.StringArg(args, "id")
			if err != nil {
				return nil, err
			}
			task, err := d.TaskRepo.GetByID(ctx, id)
			if err != nil {
				return nil, mcp.Fail("Task not found: " + id)
			}
			if task.CurrentStage == "done" || task.CurrentStage == "cancelled" {
				return nil, mcp.Fail("Task is already " + task.CurrentStage)
			}

			cancelled := "cancelled"
			if _, err := d.TaskRepo.Update(ctx, id, repo.UpdateTaskInput{CurrentStage: &cancelled}); err != nil {
				return nil, mcp.Fail("cancel_task: " + err.Error())
			}
			_ = d.AuditRepo.Append(ctx, repo.AppendAuditInput{
				TaskID: id,
				Actor:  "user",
				Action: "cancelled",
			})
			if d.Orchestrator != nil {
				d.Orchestrator.NotifyTaskTerminated(ctx, id, "cancelled")
			}
			safeBroadcast(d.Broadcast, id)

			task, _ = d.TaskRepo.GetByID(ctx, id)
			return mcp.OK(map[string]any{"task": task})
		},
	})
}

func registerRetryTask(registry mcp.ToolRegistry, d ControlDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "retry_task",
		Description: "Retry a task that has a failed stage run on its current stage.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Task ID"},
			},
			"required": []string{"id"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			id, err := mcp.StringArg(args, "id")
			if err != nil {
				return nil, err
			}
			task, err := d.TaskRepo.GetByID(ctx, id)
			if err != nil {
				return nil, mcp.Fail("Task not found: " + id)
			}

			// Get the latest stage run for the task's current stage.
			latest, _ := d.SRRepo.GetLatestByTaskAndStage(ctx, id, task.CurrentStage)
			if latest == nil || latest.Status != "failed" {
				return nil, mcp.Fail("Task has no failed stage run to retry on its current stage")
			}

			_ = d.AuditRepo.Append(ctx, repo.AppendAuditInput{
				TaskID: id,
				Actor:  "user",
				Action: "retry_requested",
				Details: map[string]any{
					"stage":     latest.Stage,
					"iteration": latest.Iteration,
				},
			})

			if d.Orchestrator == nil {
				return nil, mcp.Fail("orchestrator not available")
			}
			stageRun, err := d.Orchestrator.ProgressTask(ctx, id, nil)
			if err != nil {
				return nil, mcp.Fail("retry_task: " + err.Error())
			}
			if stageRun == nil {
				return nil, mcp.Fail("Task could not progress (slot full, no handler, or terminal)")
			}
			safeBroadcast(d.Broadcast, id)
			task, _ = d.TaskRepo.GetByID(ctx, id)
			return mcp.OK(map[string]any{"task": task, "stageRun": stageRun})
		},
	})
}

func registerGrantPermission(registry mcp.ToolRegistry, d ControlDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "grant_permission",
		Description: "Grant a tool permission to a task.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Task ID"},
				"tool":    map[string]any{"type": "string", "description": "Tool name from the pipeline allow-list"},
				"pattern": map[string]any{"type": "string", "description": "Optional tool argument pattern (max 256 chars)"},
			},
			"required": []string{"task_id", "tool"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			taskID, err := mcp.StringArg(args, "task_id")
			if err != nil {
				return nil, err
			}
			tool, err := mcp.StringArg(args, "tool")
			if err != nil {
				return nil, err
			}

			if _, err := d.TaskRepo.GetByID(ctx, taskID); err != nil {
				return nil, mcp.Fail("Task not found: " + taskID)
			}
			if !pipeline.AllowedToolNames[tool] {
				return nil, mcp.Fail("tool not in allow-list: " + tool)
			}

			in := repo.CreateTaskPermissionInput{
				TaskID:      taskID,
				Tool:        tool,
				Granted:     true,
				PreApproved: true,
			}
			if p := mcp.OptionalString(args, "pattern"); p != "" {
				in.Pattern = &p
			}
			perm, err := d.PermRepo.CreateTaskPermission(ctx, in)
			if err != nil {
				return nil, mcp.Fail("grant_permission: " + err.Error())
			}
			return mcp.OK(perm)
		},
	})
}

func registerResolvePermissionRequest(registry mcp.ToolRegistry, d ControlDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "resolve_permission_request",
		Description: "Resolve a pending permission request as granted or denied.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"request_id": map[string]any{"type": "string", "description": "Permission request ID"},
				"outcome":    map[string]any{"type": "string", "enum": []string{"granted", "denied"}},
			},
			"required": []string{"request_id", "outcome"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			requestID, err := mcp.StringArg(args, "request_id")
			if err != nil {
				return nil, err
			}
			outcome, err := mcp.StringArg(args, "outcome")
			if err != nil {
				return nil, err
			}
			if outcome != "granted" && outcome != "denied" {
				return nil, mcp.Fail("outcome must be 'granted' or 'denied'")
			}

			req, err := d.PermRepo.GetPermissionRequest(ctx, requestID)
			if err != nil {
				return nil, mcp.Fail("Permission request not found: " + requestID)
			}

			if err := d.PermRepo.ResolvePermissionRequest(ctx, requestID, outcome); err != nil {
				return nil, mcp.Fail("resolve_permission_request: " + err.Error())
			}

			// If granted, create a task permission for the associated task.
			var resumed bool
			run, runErr := d.SRRepo.GetByID(ctx, req.StageRunID)
			if runErr == nil && run != nil {
				if outcome == "granted" {
					in := repo.CreateTaskPermissionInput{
						TaskID:      run.TaskID,
						Tool:        req.Tool,
						Granted:     true,
						PreApproved: false,
					}
					if req.Pattern != nil {
						in.Pattern = req.Pattern
					}
					_, _ = d.PermRepo.CreateTaskPermission(ctx, in)
				}
				if d.Orchestrator != nil {
					_, _ = d.Orchestrator.ResumeFromUser(ctx, run.TaskID)
					safeBroadcast(d.Broadcast, run.TaskID)
					resumed = true
				}
			} else if outcome == "granted" {
				// Stage run not found — log via result warning, agent not signalled.
				return mcp.OK(map[string]any{
					"resolved": req,
					"resumed":  false,
					"warning":  "Stage run not found; agent not signalled",
				})
			}

			return mcp.OK(map[string]any{"resolved": req, "resumed": resumed})
		},
	})
}
