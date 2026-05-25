package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
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
	auditRepo := repo.NewAuditEventRepo(client)
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
		// BuildTaskPayload is called inside applyTransitionWrites, bound to the
		// active transaction, so the returned snapshot reflects the just-applied
		// writes before tx.Commit(). The result is forwarded to OnTaskChanged
		// after the commit succeeds.
		BuildTaskPayload: func(ctx context.Context, taskID string, txSRRepo repo.StageRunRepo, txPermRepo repo.PermissionRepo) any {
			t, err := taskRepo.GetByID(ctx, taskID)
			if err != nil {
				slog.Warn("BuildTaskPayload: task lookup failed", "taskID", taskID, "err", err)
				return nil
			}
			enriched, err := tasks.EnrichTask(ctx, t, txSRRepo, txPermRepo)
			if err != nil {
				slog.Warn("BuildTaskPayload: enrich failed", "taskID", taskID, "err", err)
				return nil
			}
			return enriched
		},
		OnTaskChanged: func(taskID string, _ string, payload any) {
			// Broadcast stage_run_updated with the enriched task so the client can
			// apply the update directly without a round-trip refetch (F-PERF-013).
			//
			// payload is a pre-built *tasks.EnrichedTask read inside the transaction
			// (consistent, post-write state). When nil — e.g. for out-of-tx callers
			// like tryAttachSessionID — fall back to a live read.
			var enriched *tasks.EnrichedTask
			if payload != nil {
				if e, ok := payload.(*tasks.EnrichedTask); ok {
					enriched = e
				}
			}
			if enriched == nil {
				ctx := context.Background()
				t, err := taskRepo.GetByID(ctx, taskID)
				if err != nil {
					slog.Warn("OnTaskChanged: task lookup failed", "taskID", taskID, "err", err)
					return
				}
				var ferr error
				enriched, ferr = tasks.EnrichTask(ctx, t, srRepo, permRepo)
				if ferr != nil {
					slog.Warn("OnTaskChanged: enrich failed", "taskID", taskID, "err", ferr)
					return
				}
			}
			tb.Broadcast(sse.TaskEvent{
				Type:    "stage_run_updated",
				TaskID:  taskID,
				Payload: enriched,
			})
		},
	})
	return orch, err
}
