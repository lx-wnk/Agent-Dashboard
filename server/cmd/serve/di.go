// Dependency wiring — auto-constructed, not generated.
// Domain-scoped provider functions live in sibling files:
//   di_db.go       — database bundle
//   di_router.go   — router config + HTTP server
//   di_pipeline.go — orchestrator, spawners
//   di_tasks.go    — task HTTP handler
//   di_mcp.go      — MCP HTTP handler
//
// This file is the thin coordinator that assembles all domains into the final Server.

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/lx-wnk/agent-dashboard/server/internal/agentbroadcast"
	"github.com/lx-wnk/agent-dashboard/server/internal/api"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/adapters"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/agents"
	apianalytics "github.com/lx-wnk/agent-dashboard/server/internal/api/analytics"
	coordapi "github.com/lx-wnk/agent-dashboard/server/internal/api/coord"
	apicost "github.com/lx-wnk/agent-dashboard/server/internal/api/cost"
	apieval "github.com/lx-wnk/agent-dashboard/server/internal/api/eval"
	apihistory "github.com/lx-wnk/agent-dashboard/server/internal/api/history"
	planapi "github.com/lx-wnk/agent-dashboard/server/internal/api/plan"
	apiplugins "github.com/lx-wnk/agent-dashboard/server/internal/api/plugins"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/presets"
	providersapi "github.com/lx-wnk/agent-dashboard/server/internal/api/providers"
	refineapi "github.com/lx-wnk/agent-dashboard/server/internal/api/refine"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/remotes"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/search"
	settingsapi "github.com/lx-wnk/agent-dashboard/server/internal/api/settings"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/systemprompts"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
	apivisualizations "github.com/lx-wnk/agent-dashboard/server/internal/api/visualizations"
	apiwp "github.com/lx-wnk/agent-dashboard/server/internal/api/wphandler"
	authpkg "github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/config"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/rawrepo"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/eval"
	histsvc "github.com/lx-wnk/agent-dashboard/server/internal/history"
	"github.com/lx-wnk/agent-dashboard/server/internal/hookstore"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
	"github.com/lx-wnk/agent-dashboard/server/internal/permissions"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
	"github.com/lx-wnk/agent-dashboard/server/internal/pluginsctl"
	"github.com/lx-wnk/agent-dashboard/server/internal/provider"
	"github.com/lx-wnk/agent-dashboard/server/internal/providersettings"
	"github.com/lx-wnk/agent-dashboard/server/internal/refine"
	"github.com/lx-wnk/agent-dashboard/server/internal/scanner"
	"github.com/lx-wnk/agent-dashboard/server/internal/scheduler"
	"github.com/lx-wnk/agent-dashboard/server/internal/services"
	"github.com/lx-wnk/agent-dashboard/server/internal/settings"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
	wpservice "github.com/lx-wnk/agent-dashboard/server/internal/webpush"
)

// settingsRepoAdapter maps settings.Repo onto the ent AppSettingRepo.
type settingsRepoAdapter struct{ inner repo.AppSettingRepo }

func (a settingsRepoAdapter) Get(ctx context.Context, k string) (string, bool, error) {
	return a.inner.Get(ctx, k)
}

func (a settingsRepoAdapter) Set(ctx context.Context, k, v string) error {
	_, err := a.inner.Upsert(ctx, k, v)
	return err
}

func (a settingsRepoAdapter) ListAll(ctx context.Context) (map[string]string, error) {
	rows, err := a.inner.List(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		m[r.Key] = r.Value
	}
	return m, nil
}

func initializeServer(ctx context.Context, cfg config.Config, cfgFile string) (*api.Server, *sse.Broadcaster, *merger.Merger, *pipeline.PipelineOrchestrator, *scheduler.Scheduler, *histsvc.Importer, agentbroadcast.BaselineProvider, merger.Enricher, *eval.Service, func(), error) {
	bundle, err := provideDB(cfg)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	var entClient *ent.Client
	if bundle != nil {
		entClient = bundle.Client
	}

	var webPushHandler *apiwp.Handler
	if bundle != nil {
		notifCfgRepo := rawrepo.NewNotificationConfigRepo(bundle.DB)
		subRepo := rawrepo.NewPushSubscriptionRepo(bundle.DB)
		wpSvc := wpservice.NewService(notifCfgRepo, subRepo)
		webPushHandler = apiwp.NewHandler(wpSvc)
	}

	// broadcaster (agent SSE) and taskBroadcaster (task SSE) are constructed here
	// and injected into handlers. Neither the pipeline nor any sub-package holds a
	// reference to these broadcasters — all notifications flow outward via callbacks
	// registered in OrchestratorOptions (e.g. OnTaskChanged). This keeps the
	// pipeline layer free of any SSE dependency and independently testable.
	// broadcaster and taskBroadcaster are independent — never share them.
	// broadcaster pushes Agent[] snapshots to /api/agents/stream subscribers each scan cycle.
	// taskBase / taskBroadcaster handle typed TaskEvent messages on /api/tasks/stream.
	// Both use sse.Broadcaster under the hood (non-blocking fan-out, drops frames for slow consumers).
	broadcaster := sse.NewBroadcaster()

	// Single shared Merger for the whole process — owns the cross-tick stale
	// tracker, so finished-agent state must persist across every read path
	// (broadcast loop, router accessors, search). Constructing more than one
	// would split that state and lose finished cards between ticks.
	ollama := provider.NewOllamaClassifier("http://localhost:11434")
	providerRegistry, err := provider.NewRegistry(provider.Options{
		UserDir: cfg.ProviderDir,
		Ollama:  ollama,
		Pricing: merger.PricingAdapter(),
	})
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("provider registry: %w", err)
	}
	var providerSettingRepo repo.ProviderSettingRepo
	if entClient != nil {
		providerSettingRepo = repo.NewProviderSettingRepo(entClient)
	}
	providerSettingsSvc := providersettings.New(
		providerSettingRepo,
		provider.DefaultEnabled(providerRegistry.Descriptors(), cfg.ProvidersEnabled),
	)
	if providerSettingRepo != nil {
		if err := providerSettingsSvc.Load(ctx); err != nil {
			return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("provider settings load: %w", err)
		}
	}
	providerRegistry.SetEnabled(providerSettingsSvc.EnabledFunc())

	var providersHandler *providersapi.Handler
	if providerSettingRepo != nil {
		providersHandler = providersapi.NewHandler(providerRegistry, providerSettingsSvc)
	}

	var settingsSvc *settings.Service
	var settingsHandler *settingsapi.Handler
	if entClient != nil {
		appSettingRepo := repo.NewAppSettingRepo(entClient)
		settingsSvc = settings.New(settingsRepoAdapter{inner: appSettingRepo})
		if err := settingsSvc.Load(ctx); err != nil {
			return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("settings load: %w", err)
		}
		settingsHandler = settingsapi.NewHandler(settingsSvc)
	}

	agentMerger := merger.New(
		merger.WithRegistry(providerRegistry),
		merger.WithScanFn(func(ctx context.Context) ([]scanner.ProcessInfo, error) {
			return scanner.ScanProcessesWithDetector(ctx, providerRegistry)
		}),
	)

	taskBase := sse.NewBroadcaster()
	taskBroadcaster := sse.NewTaskBroadcaster(taskBase)
	spawnerBroadcaster := sse.NewSpawnerBroadcaster(sse.NewBroadcaster())
	projectBroadcaster := sse.NewProjectBroadcaster(sse.NewBroadcaster())

	// Load plugins from configured plugin_dir. ctx is the server-lifetime context
	// (cancelled on SIGTERM/SIGINT). Load derives a 30-second startup timeout internally.
	pluginRegistry := plugin.New(cfg.PluginDir)

	// oauthProvider and pluginLoginURL are set by the SetAuth hook when an auth_provider
	// plugin passes health-check. If no auth_provider plugin is configured both stay at
	// zero values, which activates bypass-auth on loopback.
	var oauthProvider authpkg.OAuthProvider
	var pluginLoginURL string
	if err := pluginRegistry.Load(ctx, plugin.Hooks{
		SetAuth: func(p authpkg.OAuthProvider, loginURL string) {
			oauthProvider = p
			pluginLoginURL = loginURL
			slog.Info("auth: using plugin provider", "loginURL", loginURL)
		},
	}); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("plugin registry: load failed: %w", err)
	}
	cleanup := func() { pluginRegistry.Shutdown() }

	// Fatal-safety check: if a plugin directory is configured AND at least one
	// plugin.json declared auth_provider capability BUT no healthy auth_provider
	// ended up in the registry, the server must not start — booting without auth
	// would be a silent security regression.
	if pluginRegistry.HasDir() &&
		pluginRegistry.HasAttemptedCapability(plugin.CapAuthProvider) &&
		pluginRegistry.FindByCapability(plugin.CapAuthProvider) == nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, cleanup, fmt.Errorf(
			"auth_provider plugin configured but failed health-check — refusing to start with auth disabled",
		)
	}

	if oauthProvider == nil {
		slog.Info("auth: no auth_provider plugin found — bypass-auth active for loopback")
	}

	// Plugin enable/disable control plane. settingsSvc may be nil (no DB): the
	// controller then reports nothing enabled and rejects writes.
	pluginsHandler := apiplugins.New(pluginsctl.New(pluginRegistry, settingsSvc, cfg.PluginDir))

	routerConfig := provideRouterConfig(cfg, oauthProvider, pluginLoginURL)

	var systemPromptRepo repo.SystemPromptRepo
	if entClient != nil {
		systemPromptRepo = repo.NewSystemPromptRepo(entClient)
	}

	// Construct repos required by the spawner resolver BEFORE the
	// orchestrator so the resolver can be threaded into stage handlers.
	// Seed claude-default first so the resolver's deployment-wide fallback
	// is guaranteed to exist for every task that lacks an explicit ref.
	var taskRepoForResolver repo.TaskRepo
	var projectRepo repo.ProjectRepo
	var projectFolderRepo repo.ProjectFolderRepo
	var spawnerRepo repo.SpawnerRepo
	var spawnerResolver services.SpawnerResolver
	if entClient != nil {
		taskRepoForResolver = repo.NewTaskRepo(entClient)
		projectRepo = repo.NewProjectRepo(entClient)
		projectFolderRepo = repo.NewProjectFolderRepo(entClient)
		spawnerRepo = repo.NewSpawnerRepo(entClient)
		if bundle != nil {
			if err := repairSpawnerAdapterConfig(ctx, bundle.DB); err != nil {
				return nil, nil, nil, nil, nil, nil, nil, nil, nil, cleanup, fmt.Errorf("repair spawner adapter_config: %w", err)
			}
		}
		if err := seedSpawners(ctx, spawnerRepo); err != nil {
			return nil, nil, nil, nil, nil, nil, nil, nil, nil, cleanup, fmt.Errorf("seed spawners: %w", err)
		}
		if err := migrateAdapterConfigToSpawners(ctx, cfg, spawnerRepo); err != nil {
			return nil, nil, nil, nil, nil, nil, nil, nil, nil, cleanup, fmt.Errorf("migrate adapter config: %w", err)
		}
		pipelineConfigRepo := repo.NewPipelineConfigRepo(entClient)
		spawnerResolver = services.NewSpawnerResolver(taskRepoForResolver, projectRepo, spawnerRepo, pipelineConfigRepo)
	}

	orch, err := provideOrchestrator(cfg, entClient, taskBroadcaster, systemPromptRepo, spawnerResolver)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, cleanup, err
	}

	// Construct the detached refinement runner before the task handler so the
	// nil-safe interface value (refineReaderArg) can be threaded in. The runner
	// itself is late-bound to taskHandler.BroadcastTaskUpdate after both are ready.
	var refineRunner *refine.Runner
	if entClient != nil {
		refineRunner = refine.NewRunner(repo.NewRefinementTurnRepo(entClient), nil)
	}

	// Nil-interface guard: a nil *refine.Runner assigned directly to the interface
	// produces a non-nil interface value (typed nil), which defeats the
	// `h.refineReader == nil` guard in applyRefineStatus. Compute a true nil
	// interface when there is no runner.
	var refineReaderArg tasks.RefineStatusReader
	if refineRunner != nil {
		refineReaderArg = refineRunner
	}

	var rawDB *sql.DB
	if bundle != nil {
		rawDB = bundle.DB
	}
	taskHandler := provideTaskHandler(entClient, rawDB, orch, taskBroadcaster, refineReaderArg)

	// Scheduler: recurring task firing engine + its REST handler. Reuses the task
	// handler's create core, so it must be built after taskHandler. nil when no DB.
	sched, schedulesHandler := provideScheduler(entClient, taskHandler, taskBroadcaster)

	mcpHandler := provideMCPHandler(entClient, orch, sched, taskBroadcaster, refineRunner)

	var histImporter *histsvc.Importer
	var historyHandler *apihistory.Handler
	if entClient != nil {
		costRepo := repo.NewAgentCostTrendRepo(entClient)
		costProjectResolver := services.NewCostProjectResolver(projectFolderRepo)
		histImporter = histsvc.NewImporter(costRepo).WithProjectResolver(costProjectResolver)
		historyHandler = apihistory.NewHandler(histImporter)
	}

	// Eval / drift-detection subsystem. The onDrift callback is the only outward
	// hook — eval/ stays notifications-agnostic; wiring lives here in the root.
	var evalService *eval.Service
	var evalHandler *apieval.Handler
	if entClient != nil {
		evalMetricRepo := repo.NewEvalMetricRepo(entClient)
		driftAlertRepo := repo.NewDriftAlertRepo(entClient)
		collector := eval.NewCollector(repo.NewStageRunRepo(entClient), repo.NewTaskRepo(entClient))
		evalService = eval.NewService(
			collector,
			evalMetricRepo,
			driftAlertRepo,
			eval.Thresholds{RateDropPP: cfg.EvalRateDropPP, StddevK: cfg.EvalStddevK, MinSamples: cfg.EvalMinSamples},
			cfg.EvalWindowHours,
		).WithOnDrift(func(findings []eval.DriftFinding) {
			for _, f := range findings {
				slog.Warn("eval: agent drift detected",
					"stage", f.Dim.Stage, "spawnerID", f.Dim.SpawnerID, "model", f.Dim.Model,
					"metric", f.MetricKey, "direction", f.Direction,
					"baseline", f.BaselineValue, "recent", f.RecentValue, "delta", f.Delta)
			}
		})
		evalHandler = apieval.NewHandler(evalMetricRepo, driftAlertRepo, evalService)
	}

	var refineHandler *refineapi.Handler
	if entClient != nil {
		refineHandler = refineapi.NewHandler(refineapi.Deps{
			Turns:     repo.NewRefinementTurnRepo(entClient),
			Tasks:     repo.NewTaskRepo(entClient),
			StageRuns: repo.NewStageRunRepo(entClient),
			Runner:    refineRunner,
			Advance: func(ctx context.Context, taskID string) error {
				_, err := orch.ProgressTask(ctx, taskID, nil)
				return err
			},
			ResolveSpawner: func(ctx context.Context, taskID string) (*ent.Spawner, services.SpawnerSource, error) {
				if spawnerResolver == nil {
					return nil, services.SpawnerSourceDefault, nil
				}
				// Refinement is not a per-stage-configurable stage; resolve with empty
				// stage so it falls through to task -> project default -> claude default.
				return spawnerResolver.Resolve(ctx, taskID, "")
			},
		})
	}

	var planHandler *planapi.Handler
	if entClient != nil {
		planHandler = planapi.NewHandler(planapi.HandlerDeps{
			Turns:     repo.NewRefinementTurnRepo(entClient),
			Tasks:     repo.NewTaskRepo(entClient),
			StageRuns: repo.NewStageRunRepo(entClient),
			Advance: func(ctx context.Context, taskID string) error {
				_, err := orch.ProgressTask(ctx, taskID, nil)
				return err
			},
			Requeue: func(ctx context.Context, taskID, prompt string) error {
				_, err := orch.RequeueForUser(ctx, taskID, prompt)
				return err
			},
		})
	}

	// Late-bind the runner → task-handler status broadcast. Must happen after both
	// are constructed; safe to call multiple times (last write wins in the runner).
	if refineRunner != nil && taskHandler != nil {
		refineRunner.SetOnRunChange(taskHandler.BroadcastTaskUpdate)
	}

	var analyticsHandler *apianalytics.Handler
	var analyticsRepo rawrepo.AnalyticsRepo
	if bundle != nil {
		cfgRepo := repo.NewPipelineConfigRepo(entClient)
		analyticsRepo = rawrepo.NewAnalyticsRepo(bundle.DB)
		analyticsHandler = apianalytics.NewHandler(analyticsRepo, bundle.DB, cfgRepo)
		// Seed project-configured extra safe Bash commands so grants validate
		// against the stored allow-list from first request onward.
		raw := cfgRepo.GetString(ctx, "extraSafeBashCommands", "")
		permissions.SetExtraSafeBashCommands(permissions.ParseExtraSafeBashCommands(raw))
	}
	// Cost baseline for the agent health score's cost-spike component. Nil repo
	// (no database) yields a provider that returns 0 → no cost penalty.
	baselineProvider := agentbroadcast.NewCostBaselineProvider(analyticsRepo)

	// Pipeline-task enricher: read-only crossing that annotates each scanned agent
	// with its linked pipeline task (ID + title). Nil entClient (no database)
	// leaves it nil → no enrichment. Threaded into BOTH GetAgents call sites
	// (the SSE broadcast loop below and the router's request-scoped accessor) so
	// the crossing is applied consistently.
	var pipelineEnricher merger.Enricher
	if entClient != nil {
		pipelineEnricher = agentbroadcast.NewPipelineTaskEnricher(repo.NewStageRunRepo(entClient), taskRepoForResolver, repo.NewPermissionRepo(entClient))
	}

	// Hook-event store + enricher: the opt-in receiver records per-event hook
	// granularity here (write side in the hooks API), and this enricher reads it
	// back onto each agent. Always constructed — hooks work without a database.
	hookStore := hookstore.New(cfg.HookEventsPerSession, hookstore.DefaultTTL)

	// Combine the read-only crossings into one enricher applied at every GetAgents
	// call site. A nil pipelineEnricher (no DB) composes away.
	agentEnricher := merger.ChainEnrichers(pipelineEnricher, agentbroadcast.NewHookEventEnricher(hookStore))

	// Built here (not earlier) so it captures agentEnricher — admin agent search
	// results carry the same pipeline-task and hook-event annotations as /api/agents.
	var searchHandler *search.Handler
	if bundle != nil {
		searchHandler = search.NewHandler(rawrepo.NewSearchRepo(bundle.DB), agentMerger, agentEnricher)
	}

	var costHandler *apicost.Handler
	if bundle != nil {
		costHandler = apicost.NewHandler(bundle.DB)
	}

	// Build optional handlers that previously lived inside provideRouterDeps.
	// projectRepo, projectFolderRepo, spawnerRepo were constructed earlier
	// for the spawner resolver — reuse those instances here.
	var userRepo repo.UserRepo
	var apiKeyRepo repo.ApiKeyRepo
	if entClient != nil {
		userRepo = repo.NewUserRepo(entClient)
		apiKeyRepo = repo.NewApiKeyRepo(entClient)
	}
	var remotesHandler *remotes.Handler
	if entClient != nil {
		remotesHandler = remotes.NewHandler(repo.NewRemoteRegistrationRepo(entClient))
	}
	var presetsHandler *presets.Handler
	var permissionPresetRepo repo.PermissionPresetRepo
	if entClient != nil {
		permissionPresetRepo = repo.NewPermissionPresetRepo(entClient)
		presetsHandler = presets.NewHandler(permissionPresetRepo)
	}
	var systemPromptsHandler *systemprompts.Handler
	if entClient != nil {
		systemPromptsHandler = systemprompts.NewHandler(systemPromptRepo)
	}
	adapterHandler := adapters.NewHandler()
	replyStore := agents.NewReplyStore()
	var channelStageOutputHandler *agents.ChannelStageOutputHandler
	var auditEventRepo repo.AuditEventRepo
	if entClient != nil {
		channelStageOutputHandler = agents.NewChannelStageOutputHandler(repo.NewStageRunRepo(entClient), apiKeyRepo)
		auditEventRepo = repo.NewAuditEventRepo(entClient)
	}

	routerDeps := api.RouterDeps{
		Ctx:                   ctx,
		Config:                routerConfig,
		AgentBroadcaster:      broadcaster,
		Merger:                agentMerger,
		Enricher:              agentEnricher,
		HookStore:             hookStore,
		OAuthProvider:         oauthProvider,
		UserRepo:              userRepo,
		ApiKeyRepo:            apiKeyRepo,
		ProjectRepo:           projectRepo,
		ProjectFolderRepo:     projectFolderRepo,
		SpawnerRepo:           spawnerRepo,
		SpawnerBroadcaster:    spawnerBroadcaster,
		ProjectBroadcaster:    projectBroadcaster,
		TaskProjectOps:        newTaskProjectOps(entClient),
		TaskHandler:           taskHandler,
		CoordHandler:          coordapi.New(repo.NewScratchpadRepo(entClient), repo.NewCoordLockRepo(entClient)),
		WebPushHandler:        webPushHandler,
		RemotesHandler:        remotesHandler,
		PresetsHandler:        presetsHandler,
		PermissionPresetRepo:  permissionPresetRepo,
		SystemPromptsHandler:  systemPromptsHandler,
		AdapterHandler:        adapterHandler,
		ProvidersHandler:      providersHandler,
		SettingsHandler:       settingsHandler,
		SearchHandler:         searchHandler,
		HistoryHandler:        historyHandler,
		RefineHandler:         refineHandler,
		PlanHandler:           planHandler,
		AnalyticsHandler:      analyticsHandler,
		CostHandler:           costHandler,
		EvalHandler:           evalHandler,
		VisualizationsHandler: apivisualizations.NewHandler(),
		MCPHandler:            mcpHandler,
		SchedulesHandler:      schedulesHandler,
		ChannelReply:          agents.NewChannelReplyHandler(replyStore, apiKeyRepo, repo.NewStageRunRepo(entClient)),
		ChannelStageOutput:    channelStageOutputHandler,
		PluginRegistry:        pluginRegistry,
		PluginsHandler:        pluginsHandler,
		AuditEventRepo:        auditEventRepo,
	}
	router := api.NewRouter(routerDeps)
	server := provideServer(cfg, router)
	return server, broadcaster, agentMerger, orch, sched, histImporter, baselineProvider, agentEnricher, evalService, cleanup, nil
}
