// Dependency wiring — manually maintained.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/lx-wnk/agent-dashboard/server/internal/api"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/agents"
	apianalytics "github.com/lx-wnk/agent-dashboard/server/internal/api/analytics"
	apihistory "github.com/lx-wnk/agent-dashboard/server/internal/api/history"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/presets"
	refineapi "github.com/lx-wnk/agent-dashboard/server/internal/api/refine"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/remotes"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/search"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
	apiwp "github.com/lx-wnk/agent-dashboard/server/internal/api/wphandler"
	authpkg "github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/config"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
	mcptools "github.com/lx-wnk/agent-dashboard/server/internal/mcp/tools"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/rawrepo"
	histsvc "github.com/lx-wnk/agent-dashboard/server/internal/history"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
	wpservice "github.com/lx-wnk/agent-dashboard/server/internal/webpush"
)

func initializeServer(ctx context.Context, cfg config.Config) (*api.Server, *sse.Broadcaster, *pipeline.PipelineOrchestrator, func(), error) {
	bundle, err := provideDB(cfg)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var entClient *ent.Client
	if bundle != nil {
		entClient = bundle.Client
	}

	var searchHandler *search.Handler
	if bundle != nil {
		searchHandler = search.NewHandler(rawrepo.NewSearchRepo(bundle.DB))
	}

	var webPushHandler *apiwp.Handler
	if bundle != nil {
		notifCfgRepo := rawrepo.NewNotificationConfigRepo(bundle.DB)
		subRepo := rawrepo.NewPushSubscriptionRepo(bundle.DB)
		wpSvc := wpservice.NewService(notifCfgRepo, subRepo)
		webPushHandler = apiwp.NewHandler(wpSvc)
	}

	broadcaster := sse.NewBroadcaster()
	taskBase := sse.NewBroadcaster()
	taskBroadcaster := sse.NewTaskBroadcaster(taskBase)

	// Load plugins from configured plugin_dir.
	// ctx is the startup context (short-lived); context.Background() is used as the
	// server-lifetime context for watchPlugin goroutines.
	// TODO: replace context.Background() with a real server-shutdown context once one
	// is threaded through initializeServer (tracked in feat/plugin-system).
	pluginRegistry := plugin.New(cfg.PluginDir)
	if err := pluginRegistry.Load(ctx, context.Background()); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("plugin registry: load failed: %w", err)
	}
	cleanup := func() { pluginRegistry.Shutdown() }

	// Fatal-safety check: if a plugin directory is configured AND at least one
	// plugin.json declared auth_provider capability BUT no healthy auth_provider
	// ended up in the registry, the server must not start — booting without auth
	// would be a silent security regression.
	if pluginRegistry.HasDir() &&
		pluginRegistry.HasAttemptedCapability(plugin.CapAuthProvider) &&
		pluginRegistry.FindByCapability(plugin.CapAuthProvider) == nil {
		return nil, nil, nil, cleanup, fmt.Errorf(
			"auth_provider plugin configured but failed health-check — refusing to start with auth disabled",
		)
	}

	// Wire auth provider: prefer plugin, fall back to bypass-auth.
	var oauthProvider authpkg.OAuthProvider
	if entry := pluginRegistry.FindByCapability(plugin.CapAuthProvider); entry != nil {
		oauthProvider = plugin.NewAuthProvider(*entry)
		slog.Info("auth: using plugin provider", "plugin", entry.Descriptor.ID)
	} else {
		slog.Info("auth: no auth_provider plugin found — bypass-auth active for loopback")
	}

	routerConfig := provideRouterConfig(cfg, oauthProvider)

	orch, err := provideOrchestrator(cfg, entClient, taskBroadcaster)
	if err != nil {
		return nil, nil, nil, cleanup, err
	}

	taskHandler := provideTaskHandler(entClient, orch, taskBroadcaster)
	mcpHandler := provideMCPHandler(entClient, orch, taskBroadcaster)

	var historyHandler *apihistory.Handler
	if entClient != nil {
		costRepo := repo.NewAgentCostTrendRepo(entClient)
		histImporter := histsvc.NewImporter(costRepo)
		historyHandler = apihistory.NewHandler(histImporter)
	}

	var refineHandler *refineapi.Handler
	if entClient != nil {
		refineHandler = refineapi.NewHandler(repo.NewRefinementTurnRepo(entClient), repo.NewTaskRepo(entClient))
	}

	var analyticsHandler *apianalytics.Handler
	if bundle != nil {
		cfgRepo := repo.NewPipelineConfigRepo(entClient)
		analyticsHandler = apianalytics.NewHandler(rawrepo.NewAnalyticsRepo(bundle.DB), bundle.DB, cfgRepo)
	}

	routerDeps := provideRouterDeps(ctx, cfg, routerConfig, broadcaster, entClient, taskHandler, mcpHandler, searchHandler, webPushHandler, historyHandler, refineHandler, analyticsHandler, pluginRegistry, oauthProvider)
	router := api.NewRouter(routerDeps)
	server := provideServer(cfg, router)
	return server, broadcaster, orch, cleanup, nil
}

func provideDB(cfg config.Config) (*db.DBBundle, error) {
	return db.Open(cfg.DBPath)
}

func provideRouterConfig(cfg config.Config, oauthProvider authpkg.OAuthProvider) api.RouterConfig {
	bypassAuth := cfg.IsLoopback() && oauthProvider == nil
	if bypassAuth {
		slog.Info("auth bypass active — loopback + no auth_provider plugin configured; all API requests allowed without login")
	}
	return api.RouterConfig{
		JWTSecret:         cfg.JWTSecret,
		CallbackURL:       cfg.CallbackURL(),
		IsLoopback:        cfg.IsLoopback(),
		BypassAuth:        bypassAuth,
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
		Client:         client,
		TaskRepo:       taskRepo,
		StageRunRepo:   srRepo,
		PermissionRepo: permRepo,
		AuditRepo:      auditRepo,
		ConfigRepo:     cfgRepo,
		MCPToken:       cfg.MCPToken,
		MCPUrl:         fmt.Sprintf("http://127.0.0.1:%d", cfg.Port),
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
		DepRepo:      repo.NewDependencyRepo(client),
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

func provideRouterDeps(ctx context.Context, cfg config.Config, rc api.RouterConfig, b *sse.Broadcaster, client *ent.Client, taskHandler *tasks.Handler, mcpHandler http.Handler, searchHandler *search.Handler, webPushHandler *apiwp.Handler, historyHandler *apihistory.Handler, refineHandler *refineapi.Handler, analyticsHandler *apianalytics.Handler, pluginRegistry *plugin.Registry, oauthProvider authpkg.OAuthProvider) api.RouterDeps {
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
	_ = cfg // cfg is retained for future use; config values consumed via RouterConfig
	return api.RouterDeps{
		Ctx:              ctx,
		Config:           rc,
		AgentBroadcaster: b,
		OAuthProvider:    oauthProvider,
		UserRepo:         userRepo,
		ApiKeyRepo:       apiKeyRepo,
		TaskHandler:      taskHandler,
		WebPushHandler:   webPushHandler,
		RemotesHandler:   remotesHandler,
		PresetsHandler:   presetsHandler,
		SearchHandler:    searchHandler,
		HistoryHandler:   historyHandler,
		RefineHandler:    refineHandler,
		AnalyticsHandler: analyticsHandler,
		MCPHandler:       mcpHandler,
		ChannelReply:     agents.NewChannelReplyHandler(replyStore),
		PluginRegistry:   pluginRegistry,
	}
}

func provideServer(cfg config.Config, handler http.Handler) *api.Server {
	return api.NewServer(cfg.Addr(), handler, cfg.ShutdownTimeout())
}
