package tools

import (
	"context"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
)

// ReadDeps holds the repositories required by the read tools.
type ReadDeps struct {
	TaskRepo    repo.TaskRepo
	SRRepo      repo.StageRunRepo
	PermRepo    repo.PermissionRepo
	AuditRepo   repo.AuditEventRepo
	ProjectRepo repo.ProjectRepo
	SpawnerRepo repo.SpawnerRepo
}

// RegisterReadTools registers all read tools into the given registry.
func RegisterReadTools(registry mcp.ToolRegistry, d ReadDeps) {
	registerListTasks(registry, d)
	registerGetTask(registry, d)
	registerListStageRuns(registry, d)
	registerListAudit(registry, d)
	registerListPermissionRequests(registry, d)
	registerListProjects(registry, d)
	registerListSpawners(registry, d)
}

const readIsoFormat = "2006-01-02T15:04:05Z"

func readTsFmt(t time.Time) string { return t.UTC().Format(readIsoFormat) }

// projectView is the JSON shape returned by the list_projects MCP tool.
// Mirrors the camelCase wire shape used by the projects HTTP handler.
type projectView struct {
	ID               string  `json:"id"`
	Slug             string  `json:"slug"`
	Name             string  `json:"name"`
	Description      *string `json:"description,omitempty"`
	Color            *string `json:"color,omitempty"`
	DefaultSpawnerID *string `json:"defaultSpawnerId,omitempty"`
	FolderCount      int     `json:"folderCount"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
}

// spawnerView is the JSON shape returned by the list_spawners MCP tool.
// Mirrors the camelCase wire shape used by the spawners HTTP handler.
type spawnerView struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Slug          string            `json:"slug"`
	Command       string            `json:"command"`
	Args          []string          `json:"args"`
	Env           map[string]string `json:"env"`
	ModelOverride *string           `json:"modelOverride,omitempty"`
	Description   *string           `json:"description,omitempty"`
	BuiltIn       bool              `json:"builtIn"`
	CreatedAt     string            `json:"createdAt"`
	UpdatedAt     string            `json:"updatedAt"`
}

// registerListProjects registers the list_projects tool.
// Returns an array of project views, each with a `folderCount` field.
// Scope: tasks:read.
func registerListProjects(registry mcp.ToolRegistry, d ReadDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "list_projects",
		Description: "List all projects with folder counts.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Handler: func(ctx context.Context, _ map[string]any) (*mcp.ToolResult, error) {
			if d.ProjectRepo == nil {
				return nil, mcp.Fail("list_projects: project repository not configured")
			}
			rows, err := d.ProjectRepo.ListWithFolderCount(ctx)
			if err != nil {
				return nil, mcp.Fail("list_projects: " + err.Error())
			}
			result := make([]projectView, len(rows))
			for i, r := range rows {
				result[i] = projectView{
					ID:               r.ID,
					Slug:             r.Slug,
					Name:             r.Name,
					Description:      r.Description,
					Color:            r.Color,
					DefaultSpawnerID: r.DefaultSpawnerID,
					FolderCount:      r.FolderCount,
					CreatedAt:        readTsFmt(r.CreatedAt),
					UpdatedAt:        readTsFmt(r.UpdatedAt),
				}
			}
			return mcp.OK(result)
		},
	})
}

// registerListSpawners registers the list_spawners tool.
// Returns an array of spawner views including built-ins.
// Scope: tasks:read.
func registerListSpawners(registry mcp.ToolRegistry, d ReadDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "list_spawners",
		Description: "List all spawners (built-in and custom).",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Handler: func(ctx context.Context, _ map[string]any) (*mcp.ToolResult, error) {
			if d.SpawnerRepo == nil {
				return nil, mcp.Fail("list_spawners: spawner repository not configured")
			}
			spawners, err := d.SpawnerRepo.List(ctx)
			if err != nil {
				return nil, mcp.Fail("list_spawners: " + err.Error())
			}
			result := make([]spawnerView, len(spawners))
			for i, s := range spawners {
				args := s.Args
				if args == nil {
					args = []string{}
				}
				env := s.Env
				if env == nil {
					env = map[string]string{}
				}
				result[i] = spawnerView{
					ID:            s.ID,
					Name:          s.Name,
					Slug:          s.Slug,
					Command:       s.Command,
					Args:          args,
					Env:           env,
					ModelOverride: s.ModelOverride,
					Description:   s.Description,
					BuiltIn:       s.BuiltIn,
					CreatedAt:     readTsFmt(s.CreatedAt),
					UpdatedAt:     readTsFmt(s.UpdatedAt),
				}
			}
			return mcp.OK(result)
		},
	})
}

// checkTaskAccess returns an error if the authenticated caller does not own the task.
// Nil userID on the task means no ownership restriction.
// MCPAuthInfo carries no UserID field, so all callers are treated as admins.
func checkTaskAccess(_ context.Context, _ *string) error {
	return nil
}

// registerListTasks registers the list_tasks tool.
// Optional "stage" argument filters by pipeline stage; without it all tasks are returned.
func registerListTasks(registry mcp.ToolRegistry, d ReadDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "list_tasks",
		Description: "List pipeline tasks, optionally filtered by stage.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"stage": map[string]any{
					"type":        "string",
					"description": "Filter by pipeline stage (e.g. concept, implementation, done).",
				},
			},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			stage := mcp.OptionalString(args, "stage")
			if stage != "" {
				tasks, err := d.TaskRepo.ListByStage(ctx, stage)
				if err != nil {
					return nil, mcp.Fail("list_tasks: " + err.Error())
				}
				if tasks == nil {
					tasks = []*ent.Task{}
				}
				return mcp.OK(tasks)
			}
			// MCPAuthInfo has no UserID — treat as admin (isAdmin=true, userID="").
			tasks, err := d.TaskRepo.ListForUser(ctx, "", true)
			if err != nil {
				return nil, mcp.Fail("list_tasks: " + err.Error())
			}
			if tasks == nil {
				tasks = []*ent.Task{}
			}
			return mcp.OK(tasks)
		},
	})
}

// registerGetTask registers the get_task tool.
// Accepts an ID or slug via the "id_or_slug" argument.
func registerGetTask(registry mcp.ToolRegistry, d ReadDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "get_task",
		Description: "Fetch a single task by ID or slug.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id_or_slug": map[string]any{
					"type":        "string",
					"description": "Task UUID or slug.",
				},
			},
			"required": []string{"id_or_slug"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			idOrSlug, err := mcp.StringArg(args, "id_or_slug")
			if err != nil {
				return nil, err
			}
			task, err := d.TaskRepo.GetByID(ctx, idOrSlug)
			if err != nil {
				if !ent.IsNotFound(err) {
					return nil, mcp.Fail("get_task: " + err.Error())
				}
				// Not found by ID — try slug
				task, err = d.TaskRepo.GetBySlug(ctx, idOrSlug)
				if err != nil {
					return nil, mcp.Fail("Task not found: " + idOrSlug)
				}
			}
			if accessErr := checkTaskAccess(ctx, task.UserID); accessErr != nil {
				return nil, accessErr
			}
			return mcp.OK(task)
		},
	})
}

// registerListStageRuns registers the list_stage_runs tool.
func registerListStageRuns(registry mcp.ToolRegistry, d ReadDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "list_stage_runs",
		Description: "List all stage runs for a task.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{
					"type":        "string",
					"description": "Task UUID.",
				},
			},
			"required": []string{"task_id"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			taskID, err := mcp.StringArg(args, "task_id")
			if err != nil {
				return nil, err
			}
			task, err := d.TaskRepo.GetByID(ctx, taskID)
			if err != nil {
				if ent.IsNotFound(err) {
					return nil, mcp.Fail("Task not found: " + taskID)
				}
				return nil, mcp.Fail("list_stage_runs: " + err.Error())
			}
			if accessErr := checkTaskAccess(ctx, task.UserID); accessErr != nil {
				return nil, accessErr
			}
			runs, err := d.SRRepo.ListForTask(ctx, taskID)
			if err != nil {
				return nil, mcp.Fail("list_stage_runs: " + err.Error())
			}
			if runs == nil {
				runs = []*ent.StageRun{}
			}
			return mcp.OK(runs)
		},
	})
}

// registerListAudit registers the list_audit tool.
func registerListAudit(registry mcp.ToolRegistry, d ReadDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "list_audit",
		Description: "List audit log entries for a task.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{
					"type":        "string",
					"description": "Task UUID.",
				},
			},
			"required": []string{"task_id"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			taskID, err := mcp.StringArg(args, "task_id")
			if err != nil {
				return nil, err
			}
			task, err := d.TaskRepo.GetByID(ctx, taskID)
			if err != nil {
				if ent.IsNotFound(err) {
					return nil, mcp.Fail("Task not found: " + taskID)
				}
				return nil, mcp.Fail("list_audit: " + err.Error())
			}
			if accessErr := checkTaskAccess(ctx, task.UserID); accessErr != nil {
				return nil, accessErr
			}
			entries, err := d.AuditRepo.ListForTask(ctx, taskID)
			if err != nil {
				return nil, mcp.Fail("list_audit: " + err.Error())
			}
			if entries == nil {
				entries = []*ent.AuditEvent{}
			}
			return mcp.OK(entries)
		},
	})
}

// registerListPermissionRequests registers the list_permission_requests tool.
func registerListPermissionRequests(registry mcp.ToolRegistry, d ReadDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "list_permission_requests",
		Description: "List all pending permission requests across every stage run of a task.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{
					"type":        "string",
					"description": "Task UUID.",
				},
			},
			"required": []string{"task_id"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			taskID, err := mcp.StringArg(args, "task_id")
			if err != nil {
				return nil, err
			}
			task, err := d.TaskRepo.GetByID(ctx, taskID)
			if err != nil {
				if ent.IsNotFound(err) {
					return nil, mcp.Fail("Task not found: " + taskID)
				}
				return nil, mcp.Fail("list_permission_requests: " + err.Error())
			}
			if accessErr := checkTaskAccess(ctx, task.UserID); accessErr != nil {
				return nil, accessErr
			}
			runs, err := d.SRRepo.ListForTask(ctx, taskID)
			if err != nil {
				return nil, mcp.Fail("list_permission_requests: " + err.Error())
			}
			requests := make([]*ent.PermissionRequest, 0)
			for _, run := range runs {
				pending, err := d.PermRepo.ListPendingForStageRun(ctx, run.ID)
				if err != nil {
					return nil, mcp.Fail("list_permission_requests: " + err.Error())
				}
				requests = append(requests, pending...)
			}
			return mcp.OK(requests)
		},
	})
}
