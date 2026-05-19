package main

import (
	"net/http"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
	mcptools "github.com/lx-wnk/agent-dashboard/server/internal/mcp/tools"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

func provideMCPHandler(client *ent.Client, orch *pipeline.PipelineOrchestrator, tb *sse.TaskBroadcaster) http.Handler {
	if client == nil || orch == nil {
		return nil
	}

	taskRepo := repo.NewTaskRepo(client)
	srRepo := repo.NewStageRunRepo(client)
	permRepo := repo.NewPermissionRepo(client)
	auditRepo := repo.NewAuditRepo(client)
	depRepo := repo.NewDependencyRepo(client)
	apiKeyRepo := repo.NewApiKeyRepo(client)

	broadcast := func(taskID string) {
		tb.Broadcast(sse.TaskEvent{Type: "task_changed", TaskID: taskID, Payload: map[string]string{}})
	}
	broadcastDeleted := func(taskID string) {
		tb.Broadcast(sse.TaskEvent{Type: "task_deleted", TaskID: taskID, Payload: map[string]string{}})
	}

	registry := mcp.ToolRegistry{}
	mcptools.RegisterReadTools(registry, mcptools.ReadDeps{
		TaskRepo:  taskRepo,
		SRRepo:    srRepo,
		PermRepo:  permRepo,
		AuditRepo: auditRepo,
	})
	mcptools.RegisterWriteTools(registry, mcptools.WriteDeps{
		TaskRepo:         taskRepo,
		PermRepo:         permRepo,
		AuditRepo:        auditRepo,
		DepRepo:          depRepo,
		Broadcast:        broadcast,
		BroadcastDeleted: broadcastDeleted,
	})
	mcptools.RegisterControlTools(registry, mcptools.ControlDeps{
		TaskRepo:     taskRepo,
		SRRepo:       srRepo,
		PermRepo:     permRepo,
		AuditRepo:    auditRepo,
		Orchestrator: orch,
		Broadcast:    broadcast,
	})
	mcptools.RegisterKeyTools(registry, mcptools.KeyDeps{
		ApiKeyRepo: apiKeyRepo,
	})
	return mcp.MCPHandler(registry)
}
