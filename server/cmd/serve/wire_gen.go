// Dependency wiring — manually maintained (Wire is not used).

package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/lx-wnk/agent-dashboard/server/internal/api"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/agents"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/presets"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/remotes"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/search"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
	authpkg "github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/config"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
	mcptools "github.com/lx-wnk/agent-dashboard/server/internal/mcp/tools"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

func initializeServer(cfg config.Config) (*api.Server, *sse.Broadcaster, *pipeline.PipelineOrchestrator, error) {
	bundle, err := provideDB(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	entClient := bundle.Client

	var searchHandler *search.Handler
	if bundle != nil {
		searchHandler = search.NewHandler(bundle.DB)
	}

	broadcaster := sse.NewBroadcaster()
	taskBroadcaster := sse.NewTaskBroadcaster(broadcaster)
	routerConfig := provideRouterConfig(cfg)

	orch, err := provideOrchestrator(cfg, entClient, taskBroadcaster)
	if err != nil {
		return nil, nil, nil, err
	}

	taskHandler := provideTaskHandler(entClient, orch, taskBroadcaster)
	mcpHandler := provideMCPHandler(entClient, orch, taskBroadcaster)
	routerDeps := provideRouterDeps(cfg, routerConfig, broadcaster, entClient, taskHandler, mcpHandler, searchHandler)
	router := api.NewRouter(routerDeps)
	server := provideServer(cfg, router)
	return server, broadcaster, orch, nil
}

func provideDB(cfg config.Config) (*db.DBBundle, error) {
	return db.Open(cfg.DBPath)
}

func provideGitHubClient(cfg config.Config) *authpkg.GitHubClient {
	if cfg.GitHubClientID == "" {
		return nil
	}
	return authpkg.NewGitHubClient(cfg.GitHubClientID, cfg.GitHubClientSecret)
}

func provideRouterConfig(cfg config.Config) api.RouterConfig {
	return api.RouterConfig{
		JWTSecret:         cfg.JWTSecret,
		CallbackURL:       cfg.CallbackURL(),
		IsLoopback:        cfg.IsLoopback(),
		HooksSecret:       cfg.HooksSecret,
		HooksDebounceMs:   cfg.HooksDebounceMs,
		SpawnRateLimit:    cfg.SpawnRateLimit,
		SpawnRateWindowMs: cfg.SpawnRateWindowMs,
	}
}

func provideOrchestrator(cfg config.Config, client *ent.Client, tb *sse.TaskBroadcaster) (*pipeline.PipelineOrchestrator, error) {
	if client == nil {
		return nil, nil
	}
	taskRepo := repo.NewTaskRepo(client)
	srRepo := repo.NewStageRunRepo(client)
	permRepo := repo.NewPermissionRepo(client)
	auditRepo := repo.NewAuditRepo(client)
	cfgRepo := repo.NewPipelineConfigRepo(client)

	orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
		TaskRepo:       taskRepo,
		StageRunRepo:   srRepo,
		PermissionRepo: permRepo,
		AuditRepo:      auditRepo,
		ConfigRepo:     cfgRepo,
		MCPToken:       os.Getenv("DASHBOARD_MCP_TOKEN"),
		MCPUrl:         fmt.Sprintf("http://%s:%d", cfg.Host, cfg.Port),
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

func provideTaskHandler(client *ent.Client, orch *pipeline.PipelineOrchestrator, tb *sse.TaskBroadcaster) *tasks.Handler {
	if client == nil || orch == nil {
		return nil
	}
	return tasks.NewHandler(tasks.Deps{
		TaskRepo:     repo.NewTaskRepo(client),
		SRRepo:       repo.NewStageRunRepo(client),
		PermRepo:     repo.NewPermissionRepo(client),
		AuditRepo:    repo.NewAuditRepo(client),
		CfgRepo:      repo.NewPipelineConfigRepo(client),
		Orchestrator: orch,
		Broadcaster:  tb,
	})
}

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

func provideRouterDeps(cfg config.Config, rc api.RouterConfig, b *sse.Broadcaster, client *ent.Client, taskHandler *tasks.Handler, mcpHandler http.Handler, searchHandler *search.Handler) api.RouterDeps {
	var userRepo repo.UserRepo
	var apiKeyRepo repo.ApiKeyRepo
	if client != nil {
		userRepo = repo.NewUserRepo(client)
		apiKeyRepo = repo.NewApiKeyRepo(client)
	}
	var remotesHandler *remotes.Handler
	if client != nil {
		remotesHandler = remotes.NewHandler(repo.NewRemoteRegistrationRepo(client))
	}
	var presetsHandler *presets.Handler
	if client != nil {
		presetsHandler = presets.NewHandler(repo.NewPermissionPresetRepo(client))
	}
	replyStore := agents.NewReplyStore()
	return api.RouterDeps{
		Config:           rc,
		AgentBroadcaster: b,
		GitHubClient:     provideGitHubClient(cfg),
		UserRepo:         userRepo,
		ApiKeyRepo:       apiKeyRepo,
		TaskHandler:      taskHandler,
		RemotesHandler:   remotesHandler,
		PresetsHandler:   presetsHandler,
		SearchHandler:    searchHandler,
		MCPHandler:       mcpHandler,
		ChannelReply:     agents.NewChannelReplyHandler(replyStore),
	}
}

func provideServer(cfg config.Config, handler http.Handler) *api.Server {
	return api.NewServer(cfg.Addr(), handler, cfg.ShutdownTimeout())
}
