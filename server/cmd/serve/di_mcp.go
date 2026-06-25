package main

import (
	"context"
	"net/http"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
	mcptools "github.com/lx-wnk/agent-dashboard/server/internal/mcp/tools"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/lx-wnk/agent-dashboard/server/internal/refine"
	"github.com/lx-wnk/agent-dashboard/server/internal/scheduler"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

func provideMCPHandler(
	client *ent.Client,
	orch *pipeline.PipelineOrchestrator,
	sched *scheduler.Scheduler,
	tb *sse.TaskBroadcaster,
	refineRunner *refine.Runner,
) http.Handler {
	if client == nil || orch == nil {
		return nil
	}

	taskRepo := repo.NewTaskRepo(client)
	srRepo := repo.NewStageRunRepo(client)
	permRepo := repo.NewPermissionRepo(client)
	auditRepo := repo.NewAuditEventRepo(client)
	depRepo := repo.NewDependencyRepo(client)
	apiKeyRepo := repo.NewApiKeyRepo(client)
	projectRepo := repo.NewProjectRepo(client)
	spawnerRepo := repo.NewSpawnerRepo(client)
	turnsRepo := repo.NewRefinementTurnRepo(client)

	broadcast := func(taskID string) {
		tb.Broadcast(sse.TaskEvent{Type: "task_changed", TaskID: taskID, Payload: map[string]string{}})
	}
	broadcastDeleted := func(taskID string) {
		tb.Broadcast(sse.TaskEvent{Type: "task_deleted", TaskID: taskID, Payload: map[string]string{}})
	}

	registry := mcp.ToolRegistry{}
	mcptools.RegisterReadTools(registry, mcptools.ReadDeps{
		TaskRepo:    taskRepo,
		SRRepo:      srRepo,
		PermRepo:    permRepo,
		AuditRepo:   auditRepo,
		ProjectRepo: projectRepo,
		SpawnerRepo: spawnerRepo,
	})
	mcptools.RegisterWriteTools(registry, mcptools.WriteDeps{
		TaskRepo:         taskRepo,
		PermRepo:         permRepo,
		AuditRepo:        auditRepo,
		DepRepo:          depRepo,
		ProjectRepo:      projectRepo,
		SpawnerRepo:      spawnerRepo,
		Broadcast:        broadcast,
		BroadcastDeleted: broadcastDeleted,
	})
	mcptools.RegisterControlTools(registry, mcptools.ControlDeps{
		TaskRepo:     taskRepo,
		SRRepo:       srRepo,
		PermRepo:     permRepo,
		AuditRepo:    auditRepo,
		Orchestrator: orch,
		RefineReader: refineRunner,
		Broadcast:    broadcast,
	})
	mcptools.RegisterKeyTools(registry, mcptools.KeyDeps{
		ApiKeyRepo: apiKeyRepo,
	})
	mcptools.RegisterRefineTools(registry, mcptools.RefineDeps{
		Turns:     turnsRepo,
		Tasks:     taskRepo,
		StageRuns: srRepo,
		Runner:    refineRunner,
		Advance: func(ctx context.Context, taskID string) error {
			_, err := orch.ProgressTask(ctx, taskID, nil)
			return err
		},
	})
	mcptools.RegisterPlanTools(registry, mcptools.PlanDeps{
		Turns:     turnsRepo,
		Tasks:     taskRepo,
		StageRuns: srRepo,
		Advance: func(ctx context.Context, taskID string) error {
			_, err := orch.ProgressTask(ctx, taskID, nil)
			return err
		},
		Requeue: func(ctx context.Context, taskID, prompt string) error {
			_, err := orch.RequeueForUser(ctx, taskID, prompt)
			return err
		},
	})
	mcptools.RegisterScheduleTools(registry, mcptools.ScheduleDeps{
		Repo:       repo.NewTaskScheduleRepo(client),
		Translator: scheduler.NewNLCron(nil),
		Runner:     sched,
		Broadcast:  broadcast,
	})
	return mcp.MCPHandler(registry)
}
