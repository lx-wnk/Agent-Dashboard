package main

import (
	"database/sql"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/rawrepo"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/lx-wnk/agent-dashboard/server/internal/services"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

func provideTaskHandler(client *ent.Client, db *sql.DB, orch *pipeline.PipelineOrchestrator, tb *sse.TaskBroadcaster, refineReader tasks.RefineStatusReader) *tasks.Handler {
	if client == nil || orch == nil {
		return nil
	}
	taskRepo := repo.NewTaskRepo(client)
	return tasks.NewHandler(tasks.Deps{
		Client:            client,
		TaskRepo:          taskRepo,
		SRRepo:            repo.NewStageRunRepo(client),
		SRBulkRepo:        rawrepo.NewStageRunBulkRepo(db),
		PermRepo:          repo.NewPermissionRepo(client),
		PresetRepo:        repo.NewPermissionPresetRepo(client),
		AuditRepo:         repo.NewAuditEventRepo(client),
		AuditEventRepo:    repo.NewAuditEventRepo(client),
		CfgRepo:           repo.NewPipelineConfigRepo(client),
		DepRepo:           repo.NewDependencyRepo(client),
		ProjectRepo:       repo.NewProjectRepo(client),
		ProjectFolderRepo: repo.NewProjectFolderRepo(client),
		SpawnerRepo:       repo.NewSpawnerRepo(client),
		Orchestrator:      orch,
		Broadcaster:       tb,
		WorktreeMgr:       services.NewWorktreeManager(taskRepo),
		RefineReader:      refineReader,
	})
}
