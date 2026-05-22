package main

import (
	"context"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/config"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/lx-wnk/agent-dashboard/server/internal/services"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

func provideOrchestrator(
	cfg config.Config,
	client *ent.Client,
	tb *sse.TaskBroadcaster,
	systemPromptRepo repo.SystemPromptRepo,
	spawnerResolver services.SpawnerResolver,
) (*pipeline.PipelineOrchestrator, error) {
	if client == nil {
		return nil, nil
	}
	taskRepo := repo.NewTaskRepo(client)
	srRepo := repo.NewStageRunRepo(client)
	permRepo := repo.NewPermissionRepo(client)
	auditRepo := repo.NewAuditRepo(client)
	cfgRepo := repo.NewPipelineConfigRepo(client)

	var resolveFn pipeline.SpawnerResolverFunc
	if spawnerResolver != nil {
		resolveFn = func(ctx context.Context, taskID string) (*ent.Spawner, error) {
			sp, _, err := spawnerResolver.Resolve(ctx, taskID)
			return sp, err
		}
	}

	orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
		Client:           client,
		TaskRepo:         taskRepo,
		StageRunRepo:     srRepo,
		PermissionRepo:   permRepo,
		AuditRepo:        auditRepo,
		ConfigRepo:       cfgRepo,
		SystemPromptRepo: systemPromptRepo,
		MCPToken:         cfg.MCPToken,
		MCPUrl:           fmt.Sprintf("http://127.0.0.1:%d", cfg.Port),
		WorktreeRoot:     cfg.WorktreeRoot,
		ForceWorktrees:   cfg.ForceWorktrees,
		ResolveSpawner:   resolveFn,
		OnTaskChanged: func(taskID string, transitionKind string) {
			tb.Broadcast(sse.TaskEvent{
				Type:   "task_changed",
				TaskID: taskID,
				Payload: map[string]string{
					"transitionKind": transitionKind,
				},
			})
		},
	})
	return orch, err
}
