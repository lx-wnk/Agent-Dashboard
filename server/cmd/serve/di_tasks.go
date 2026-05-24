package main

import (
	"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/lx-wnk/agent-dashboard/server/internal/services"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

func provideTaskHandler(client *ent.Client, orch *pipeline.PipelineOrchestrator, tb *sse.TaskBroadcaster) *tasks.Handler {
	if client == nil || orch == nil {
		return nil
	}
	taskRepo := repo.NewTaskRepo(client)
	return tasks.NewHandler(tasks.Deps{
		TaskRepo:          taskRepo,
		SRRepo:            repo.NewStageRunRepo(client),
		PermRepo:          repo.NewPermissionRepo(client),
		AuditRepo:         repo.NewAuditRepo(client),
		AuditEventRepo:    repo.NewAuditEventRepo(client),
		CfgRepo:           repo.NewPipelineConfigRepo(client),
		DepRepo:           repo.NewDependencyRepo(client),
		ProjectRepo:       repo.NewProjectRepo(client),
		ProjectFolderRepo: repo.NewProjectFolderRepo(client),
		SpawnerRepo:       repo.NewSpawnerRepo(client),
		Orchestrator:      orch,
		Broadcaster:       tb,
		WorktreeMgr:       services.NewWorktreeManager(taskRepo),
	})
}
