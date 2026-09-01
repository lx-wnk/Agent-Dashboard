// Dependency wiring — auto-constructed, not generated.
// Domain-scoped provider functions live in sibling files:
//   di_db.go       — database bundle
//   di_router.go   — router config + HTTP server
//   di_pipeline.go — orchestrator, spawners
//   di_tasks.go    — task HTTP handler
//   di_mcp.go      — MCP HTTP handler
//
// This file is the thin coordinator that assembles all domains into the final Server.

package serverapp

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	sdk "github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/agentbroadcast"
	"github.com/lx-wnk/agent-dashboard/server/internal/api"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/adapters"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/admin"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/agents"
	apianalytics "github.com/lx-wnk/agent-dashboard/server/internal/api/analytics"
	coordapi "github.com/lx-wnk/agent-dashboard/server/internal/api/coord"
	apicost "github.com/lx-wnk/agent-dashboard/server/internal/api/cost"
	apieval "github.com/lx-wnk/agent-dashboard/server/internal/api/eval"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/grants"
	apihistory "github.com/lx-wnk/agent-dashboard/server/internal/api/history"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/hooks"
	apimemory "github.com/lx-wnk/agent-dashboard/server/internal/api/memory"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/onboarding"
	planapi "github.com/lx-wnk/agent-dashboard/server/internal/api/plan"
	apiplugins "github.com/lx-wnk/agent-dashboard/server/internal/api/plugins"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/presets"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/prompttemplates"
	providersapi "github.com/lx-wnk/agent-dashboard/server/internal/api/providers"
	refineapi "github.com/lx-wnk/agent-dashboard/server/internal/api/refine"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/remotes"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/search"
	settingsapi "github.com/lx-wnk/agent-dashboard/server/internal/api/settings"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/systemprompts"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
	trackerapi "github.com/lx-wnk/agent-dashboard/server/internal/api/tracker"
	apiusage "github.com/lx-wnk/agent-dashboard/server/internal/api/usage"
	apivisualizations "github.com/lx-wnk/agent-dashboard/server/internal/api/visualizations"
	apiwp "github.com/lx-wnk/agent-dashboard/server/internal/api/wphandler"
	"github.com/lx-wnk/agent-dashboard/server/internal/apps/obsidian"
	"github.com/lx-wnk/agent-dashboard/server/internal/askgate"
	authpkg "github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/checkpoint"
	"github.com/lx-wnk/agent-dashboard/server/internal/claudesettings"
	"github.com/lx-wnk/agent-dashboard/server/internal/config"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/rawrepo"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/eval"
	histsvc "github.com/lx-wnk/agent-dashboard/server/internal/history"
	"github.com/lx-wnk/agent-dashboard/server/internal/hookstore"
	"github.com/lx-wnk/agent-dashboard/server/internal/memory"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
	"github.com/lx-wnk/agent-dashboard/server/internal/permissions"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
	"github.com/lx-wnk/agent-dashboard/server/internal/pluginmgmt"
	"github.com/lx-wnk/agent-dashboard/server/internal/provider"
	"github.com/lx-wnk/agent-dashboard/server/internal/providersettings"
	"github.com/lx-wnk/agent-dashboard/server/internal/refine"
	"github.com/lx-wnk/agent-dashboard/server/internal/restart"
	"github.com/lx-wnk/agent-dashboard/server/internal/scanner"
	"github.com/lx-wnk/agent-dashboard/server/internal/scheduler"
	"github.com/lx-wnk/agent-dashboard/server/internal/secretbox"
	"github.com/lx-wnk/agent-dashboard/server/internal/serverask"
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

func (a settingsRepoAdapter) SetSecret(ctx context.Context, k, ciphertext, nonce string) error {
	_, err := a.inner.UpsertSecret(ctx, k, ciphertext, nonce)
	return err
}

func (a settingsRepoAdapter) GetSecret(ctx context.Context, k string) (string, string, bool, error) {
	return a.inner.GetSecret(ctx, k)
}

// noopSettingsRepo backs the settings service when no database is configured,
// so accessors always resolve to registry defaults and consumers never nil-check.
type noopSettingsRepo struct{}

func (noopSettingsRepo) Get(context.Context, string) (string, bool, error) { return "", false, nil }
func (noopSettingsRepo) Set(context.Context, string, string) error {
	return fmt.Errorf("settings: no database configured")
}
func (noopSettingsRepo) SetSecret(context.Context, string, string, string) error {
	return fmt.Errorf("settings: no database configured")
}
func (noopSettingsRepo) GetSecret(context.Context, string) (string, string, bool, error) {
	return "", "", false, nil
}
func (noopSettingsRepo) ListAll(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

// ServerComponents bundles everything initializeServer wires up. Cleanup may be
// set even when an error is also returned (e.g. plugin registry already
// loaded) — callers must invoke it whenever it is non-nil, regardless of err.
type ServerComponents struct {
	API                 *api.Server
	Broadcaster         *sse.Broadcaster
	Merger              *merger.Merger
	Orchestrator        *pipeline.PipelineOrchestrator
	Scheduler           *scheduler.Scheduler
	HistImporter        *histsvc.Importer
	Baseline            agentbroadcast.BaselineProvider
	Enricher            merger.Enricher
	CapabilityDecisions agentbroadcast.CapabilityDecisionProvider
	Eval                *eval.Service
	Settings            *settings.Service
	// Obsidian is nil when the vault is unconfigured or no database is
	// present; see buildObsidianClient (di_obsidian.go).
	Obsidian *obsidian.Client
	Cleanup  func()
}

// ln is the address already bound by Listen, or nil to let the HTTP server
// bind cfg.Addr() itself when it starts.
func initializeServer(ctx context.Context, cfg config.Config, cfgFile string, restartCtl *restart.Controller, ln net.Listener) (*ServerComponents, error) {
	bundle, err := provideDB(cfg)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("provider registry: %w", err)
	}
	var providerSettingRepo repo.ProviderSettingRepo
	if entClient != nil {
		providerSettingRepo = repo.NewProviderSettingRepo(entClient)
	}
	providerSettingsSvc := providersettings.New(
		providerSettingRepo,
		provider.DefaultEnabled(providerRegistry.Descriptors(), nil),
	)
	if providerSettingRepo != nil {
		if err := providerSettingsSvc.Load(ctx); err != nil {
			return nil, fmt.Errorf("provider settings load: %w", err)
		}
	}
	providerRegistry.SetEnabled(providerSettingsSvc.EnabledFunc())

	var providersHandler *providersapi.Handler
	if providerSettingRepo != nil {
		providersHandler = providersapi.NewHandler(providerRegistry, providerSettingsSvc)
	}

	// Resolve the secretbox master key once, above every secret-aware
	// consumer (settings.Service here, plugin.Service below), so both share
	// one *secretbox.Box built from one key rather than two boxes that could
	// in principle diverge. Only meaningful with a database — without one
	// there is nothing to encrypt into, so box stays nil and each service's
	// own nil-box guard turns a secret read/write into a named error instead
	// of a panic.
	var box *secretbox.Box
	if entClient != nil {
		masterKey, keyErr := secretbox.LoadOrGenerateMasterKey(os.Getenv("DASHBOARD_SECRET_KEY"))
		if keyErr != nil {
			return nil, fmt.Errorf("secret master key: %w", keyErr)
		}
		var boxErr error
		box, boxErr = secretbox.New(masterKey)
		if boxErr != nil {
			return nil, fmt.Errorf("secretbox: %w", boxErr)
		}
	}

	var settingsSvc *settings.Service
	var settingsHandler *settingsapi.Handler
	if entClient != nil {
		appSettingRepo := repo.NewAppSettingRepo(entClient)
		settingsSvc = settings.New(settingsRepoAdapter{inner: appSettingRepo}, box)
		if err := settingsSvc.Load(ctx); err != nil {
			return nil, fmt.Errorf("settings load: %w", err)
		}
		settingsHandler = settingsapi.NewHandler(settingsSvc)
	} else {
		settingsSvc = settings.New(noopSettingsRepo{}, nil)
		if err := settingsSvc.Load(ctx); err != nil {
			return nil, fmt.Errorf("settings load: %w", err)
		}
	}

	// Seed the spawner command allow-list from settings (ApplyRestart).
	services.SetSpawnerAllowedCommands(settingsSvc.StringSlice("spawn.allowedCommands"))

	agentMerger := merger.New(
		merger.WithRegistry(providerRegistry),
		merger.WithScanFn(func(ctx context.Context) ([]scanner.ProcessInfo, error) {
			return scanner.ScanProcessesWithDetector(ctx, providerRegistry)
		}),
		merger.WithScreenProbe(merger.RealScreenProbe),
	)

	taskBase := sse.NewBroadcaster()
	taskBroadcaster := sse.NewTaskBroadcaster(taskBase)
	spawnerBroadcaster := sse.NewSpawnerBroadcaster(sse.NewBroadcaster())
	projectBroadcaster := sse.NewProjectBroadcaster(sse.NewBroadcaster())

	// memRepo and memRetriever back both the memory_search/memory_write MCP
	// tools (di_mcp.go) and the pipeline's push-at-spawn seam
	// (di_pipeline.go): one Retriever, constructed once, given to both
	// consumers — the same rule Retriever's own doc comment states.
	var memRepo repo.MemoryRepo
	var memRetriever *memory.Retriever
	// grantUsageRepo backs the rate-limit check inside memory.Authorize
	// (mem HTTP handler, MCP memory tools, and the pipeline's memory push
	// closure below all share it) — built once here since it needs
	// bundle.WriteClient, which only this function has in scope.
	var grantUsageRepo repo.GrantUsageRepo

	// The plugin table is the source of truth for enablement. Build the repo
	// early so the boot predicate can read it, and migrate the legacy #230
	// "plugins.enabled" setting into the table once (idempotent).
	var pluginRepo repo.PluginRepo
	// obsidianClient is nil when the vault is unconfigured (buildObsidianClient's
	// own doc comment covers why that is not an error) and stays nil without
	// a database, since Register and the capability catalogue it depends on
	// need entClient too.
	var obsidianClient *obsidian.Client
	if entClient != nil {
		memRepo = repo.NewMemoryRepo(entClient, bundle.WriteClient)
		memRetriever = memory.NewRetriever(bundle.DB, memRepo)
		grantUsageRepo = repo.NewGrantUsageRepo(entClient, bundle.WriteClient)

		pluginRepo = repo.NewPluginRepo(entClient)
		if err := seedPluginsFromEnabledList(ctx, settingsSvc, pluginRepo); err != nil {
			return nil, fmt.Errorf("seed plugins from enabled list: %w", err)
		}

		resourceRepo := repo.NewResourceRepo(entClient)
		if linked, err := repo.ReconcilePluginResources(ctx, resourceRepo, entClient); err != nil {
			slog.Warn("registry: plugin reconcile failed", "err", err)
		} else if linked > 0 {
			slog.Info("registry: linked plugins to registry identities", "count", linked)
		}

		// Seed the capability catalogue from the tool allow-list, then load it
		// back into the pipeline package so BuildAllowList's grant-translation
		// path reads real rows instead of a fabricated tool-class view. Without
		// this, the capabilities table stays empty and every lookup resolves to
		// a zero-value CapabilityView, which the gate's fail-closed default
		// sends to deny.
		capabilityRepo := repo.NewCapabilityRepo(entClient)
		if seeded := repo.SeedCapabilities(ctx, capabilityRepo); seeded > 0 {
			slog.Info("capability: seeded catalogue", "count", seeded)
		}

		// Obsidian is a builtin Application: its registry identity and its
		// four capabilities only exist once Register runs — without this
		// call, obsidian.search/obsidian.read/obsidian.write/obsidian.delete
		// all resolve to a catalogue row that was never written (class,
		// enforceable-by, reversible all zero-valued) instead of the one
		// Register declares. Idempotent, so safe on every boot.
		//
		// obsidian.IndexNotes is the function that checks obsidian.search
		// and obsidian.read against this catalogue, but nothing calls it in
		// production yet — today it is exercised only by its own package
		// tests. Client.Write and Client.Delete have no caller anywhere
		// either.
		if err := obsidian.Register(ctx, resourceRepo, capabilityRepo); err != nil {
			return nil, fmt.Errorf("obsidian: register application: %w", err)
		}

		// Construct the vault client from settings so it exists once the
		// operator configures it, rather than only once something reads
		// obsidian.IndexNotes — that call does not exist anywhere in this
		// file yet. buildObsidianClient returns nil, nil when unconfigured;
		// a half-configured vault fails the boot instead of silently
		// disabling itself (see its own doc comment for why).
		obsidianClient, err = buildObsidianClient(ctx, settingsSvc)
		if err != nil {
			return nil, fmt.Errorf("obsidian: build client: %w", err)
		}

		if rows, err := capabilityRepo.List(ctx); err != nil {
			slog.Warn("capability: catalogue load failed", "err", err)
		} else {
			catalogue := make(map[string]capability.CapabilityView, len(rows))
			for _, row := range rows {
				catalogue[row.Name] = capability.CapabilityView{
					Name:          row.Name,
					Class:         row.Class,
					EnforceableBy: row.EnforceableBy,
				}
			}
			pipeline.SetCapabilityCatalogue(catalogue)
		}
	}

	// Load plugins from configured plugin_dir. ctx is the server-lifetime context
	// (cancelled on SIGTERM/SIGINT). Load derives a 30-second startup timeout internally.
	pluginRegistry := plugin.New(cfg.PluginDir)
	activePlugins := map[string]bool{}
	if pluginRepo != nil {
		rows, listErr := pluginRepo.List(ctx)
		if listErr != nil {
			return nil, fmt.Errorf("plugin enablement snapshot: %w", listErr)
		}
		for _, p := range rows {
			activePlugins[p.ID] = p.Active
		}
	}
	pluginRegistry.SetEnabled(func(id string) bool { return activePlugins[id] })

	// Build settings service early so the provider is wired before Load.
	// Nil when running without a database (no entClient). box was resolved
	// above, alongside settings.Service — shared, not re-derived.
	var pluginSettingsSvc *plugin.Service
	if entClient != nil {
		pluginSettingRepo := repo.NewPluginSettingRepo(entClient)
		pluginSettingsSvc = plugin.NewSettingsService(pluginSettingRepoAdapter{inner: pluginSettingRepo}, box)
		pluginRegistry.SetSettingsProvider(func(ctx context.Context, id string) (map[string]string, error) {
			return pluginSettingsSvc.DecryptedAll(ctx, id)
		})
	}

	// oauthProvider and pluginLoginURL are set by the SetAuth hook when an auth_provider
	// plugin passes health-check. If no auth_provider plugin is configured both stay at
	// zero values, which activates bypass-auth on loopback.
	var oauthProvider authpkg.OAuthProvider
	var pluginLoginURL string
	if err := pluginRegistry.Load(ctx, plugin.Hooks{
		OnUnhealthy: func(id string) {
			if pluginRepo == nil {
				return
			}
			if err := pluginRepo.SetActive(ctx, id, false); err != nil {
				slog.Error("plugin: failed to persist unhealthy state", "id", id, "err", err)
			}
		},
		SetAuth: func(p authpkg.OAuthProvider, loginURL string) {
			oauthProvider = p
			pluginLoginURL = loginURL
			slog.Info("auth: using plugin provider", "loginURL", loginURL)
		},
	}); err != nil {
		return nil, fmt.Errorf("plugin registry: load failed: %w", err)
	}
	cleanup := func() { pluginRegistry.Shutdown() }

	// Fatal-safety check: if a plugin directory is configured AND at least one
	// plugin.json declared auth_provider capability BUT no healthy auth_provider
	// ended up in the registry, the server must not start — booting without auth
	// would be a silent security regression.
	if pluginRegistry.HasDir() &&
		pluginRegistry.HasAttemptedCapability(plugin.CapAuthProvider) &&
		pluginRegistry.FindByCapability(plugin.CapAuthProvider) == nil {
		return &ServerComponents{Cleanup: cleanup}, fmt.Errorf(
			"auth_provider plugin configured but failed health-check — refusing to start with auth disabled",
		)
	}

	if oauthProvider == nil {
		slog.Info("auth: no auth_provider plugin found — bypass-auth active for loopback")
	}

	// SP1 plugin lifecycle: DB-backed plugin state, per-plugin settings (secret
	// fields encrypted at rest), lifecycle transitions, and on-disk discovery.
	// Constructed only with a database — the handler stays nil otherwise.
	var pluginLifecycleHandler *apiplugins.LifecycleHandler
	if pluginSettingsSvc != nil {
		pluginSettingRepo := repo.NewPluginSettingRepo(entClient)
		lifecycleEngine := plugin.NewLifecycleEngine(
			pluginStateRepoAdapter{inner: pluginRepo},
			plugin.NewHTTPHookCaller(),
			pluginSettingsSvc,
			pluginProcessAdapter{reg: pluginRegistry},
		)
		discoverer := plugin.NewDiscoverer(cfg.PluginDir, pluginDiscoverRepoAdapter{inner: pluginRepo, settings: pluginSettingRepo})
		lifecycleProbe := func(id string) (bool, bool) {
			e, ok := pluginRegistry.Lookup(id)
			return ok, ok && e.Healthy()
		}
		lifecycleController := pluginmgmt.New(pluginRepo, lifecycleEngine, pluginSettingsSvc, cfg.PluginDir, lifecycleProbe)
		pluginLifecycleHandler = apiplugins.NewLifecycle(lifecycleController)

		if res, discErr := discoverer.Discover(ctx); discErr != nil {
			slog.Warn("plugin discovery failed", "error", discErr)
		} else {
			slog.Info("plugin discovery", "found", res.Found, "updatesAvailable", res.UpdatesAvailable)
		}
	}

	routerConfig := provideRouterConfig(cfg, settingsSvc, oauthProvider, pluginLoginURL)

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
				return &ServerComponents{Cleanup: cleanup}, fmt.Errorf("repair spawner adapter_config: %w", err)
			}
		}
		if err := seedSpawners(ctx, spawnerRepo); err != nil {
			return &ServerComponents{Cleanup: cleanup}, fmt.Errorf("seed spawners: %w", err)
		}
		if err := migrateAdapterConfigToSpawners(ctx, cfg, spawnerRepo); err != nil {
			return &ServerComponents{Cleanup: cleanup}, fmt.Errorf("migrate adapter config: %w", err)
		}
		pipelineConfigRepo := repo.NewPipelineConfigRepo(entClient)
		spawnerResolver = services.NewSpawnerResolver(taskRepoForResolver, projectRepo, spawnerRepo, pipelineConfigRepo)
	}

	// Per-turn checkpoint/revert: the Service snapshots worktree turns and drives
	// reverts; the Checkpointer runs the fsnotify watcher. orch is forward-declared
	// so the Service's KillFn can reach KillRunningStage once the orchestrator exists.
	var orch *pipeline.PipelineOrchestrator
	var checkpointSvc *checkpoint.Service
	var cpStart func(taskID, worktreePath string)
	var cpStop func(taskID string)
	if entClient != nil {
		cpRepo := repo.NewCheckpointRepo(entClient)
		cpTaskRepo := repo.NewTaskRepo(entClient)
		cpSRRepo := repo.NewStageRunRepo(entClient)
		checkpointSvc = checkpoint.NewService(checkpoint.ServiceOptions{
			Repo:        cpRepo,
			MaxPerTask:  50,
			Broadcaster: taskBroadcaster,
			KillFn: func(ctx context.Context, taskID string) error {
				if orch == nil {
					return nil
				}
				return orch.KillRunningStage(ctx, taskID)
			},
			ParkFn: func(ctx context.Context, taskID, reason string) error {
				task, err := cpTaskRepo.GetByID(ctx, taskID)
				if err != nil {
					return err
				}
				run, err := cpSRRepo.GetLatestByTaskAndStage(ctx, taskID, task.CurrentStage)
				if err != nil || run == nil {
					return err
				}
				awaiting := "awaiting_user"
				if _, err := cpSRRepo.Update(ctx, run.ID, repo.UpdateStageRunInput{
					Status:   &awaiting,
					PIDClear: true,
					Output:   map[string]any{"checkpoint_revert_reason": reason},
				}); err != nil {
					return err
				}
				taskBroadcaster.Broadcast(sse.TaskEvent{Type: "task_updated", TaskID: taskID})
				return nil
			},
		})
		cpManager := checkpoint.NewCheckpointer(checkpoint.CheckpointerOptions{
			OnSnapshot: func(taskID, worktreePath string) {
				_ = checkpointSvc.TakeSnapshot(context.Background(), taskID, worktreePath)
			},
		})
		cpStart = cpManager.Start
		cpStop = func(taskID string) {
			cpManager.Stop(taskID)
			if task, err := cpTaskRepo.GetByID(context.Background(), taskID); err == nil &&
				task != nil && task.WorktreePath != nil && *task.WorktreePath != "" {
				if err := checkpointSvc.PruneRefs(context.Background(), taskID, *task.WorktreePath); err != nil {
					slog.Warn("checkpoint: prune refs on stop", "taskID", taskID, "err", err)
				}
			}
			if err := cpRepo.DeleteByTask(context.Background(), taskID); err != nil {
				slog.Warn("checkpoint: delete rows on stop", "taskID", taskID, "err", err)
			}
		}
	}

	orch, err = provideOrchestrator(cfg, settingsSvc, entClient, taskBroadcaster, systemPromptRepo, spawnerResolver, cpStart, cpStop, memRepo, memRetriever, grantUsageRepo)
	if err != nil {
		return &ServerComponents{Cleanup: cleanup}, err
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
	taskHandler := provideTaskHandler(entClient, rawDB, orch, taskBroadcaster, refineReaderArg, settingsSvc.Bool("git.allowPull"), routerConfig.BypassAuth, checkpointSvc)

	// Scheduler: recurring task firing engine + its REST handler. Reuses the task
	// handler's create core, so it must be built after taskHandler. nil when no DB.
	sched, schedulesHandler := provideScheduler(entClient, taskHandler, taskBroadcaster, routerConfig.BypassAuth)

	// The only two callers that may block a live request on a human decision:
	// the memory MCP tools below (an agent is waiting on the tool response)
	// and the HTTP memory handler further down (a browser request has a
	// human on the other end). The pipeline's memory push (di_pipeline.go)
	// builds its own memory.Gate with no Asker instead of sharing this one —
	// nothing is waiting on it, so an unanswerable ask must deny rather than
	// stall a spawn. obsidian.IndexNotes takes a memory.Gate as a parameter
	// instead of building one (see its own doc comment); nothing in this
	// file constructs or calls it.
	var memAsker *serverask.Asker
	// Bypass-auth unmounts the route a human would answer through (router.go),
	// so an asker here would only hold every ask for its full timeout before
	// denying anyway. The router installs its debounced rescan as the asker's
	// onChange once it exists, the same split HookEnforcer already uses.
	if !routerConfig.BypassAuth {
		memAsker = serverask.New(nil)
	}
	askerArg := askerArgFor(memAsker)
	var capabilityDecisions agentbroadcast.CapabilityDecisionProvider
	if memAsker != nil {
		capabilityDecisions = func(context.Context) []sdk.PendingCapabilityDecision {
			return toPendingCapabilityDecisions(memAsker.Pending())
		}
	}

	mcpHandler := provideMCPHandler(entClient, orch, sched, taskBroadcaster, projectBroadcaster, refineRunner, memRepo, memRetriever, grantUsageRepo, askerArg)

	var histImporter *histsvc.Importer
	var historyHandler *apihistory.Handler
	if entClient != nil {
		costRepo := repo.NewAgentCostTrendRepo(entClient)
		costProjectResolver := services.NewCostProjectResolver(projectFolderRepo)
		histImporter = histsvc.NewImporter(costRepo).WithProjectResolver(costProjectResolver)
		historyHandler = apihistory.NewHandler(histImporter)
	}

	// Memory HTTP surface — reuses the single memRepo/memRetriever built above
	// (same rule as the MCP tools and the pipeline's push-at-spawn seam) plus
	// its own capability/grant repos, matching provideMCPHandler's wiring.
	var memoryHandler *apimemory.Handler
	if entClient != nil {
		memoryHandler = apimemory.NewHandler(memRepo, memRetriever, memory.Gate{
			Capabilities: repo.NewCapabilityRepo(entClient),
			Grants:       repo.NewGrantRepo(entClient),
			GrantUsage:   grantUsageRepo,
			Asker:        askerArg,
		})
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
			eval.Thresholds{RateDropPP: settingsSvc.Float("eval.rateDropPP"), StddevK: settingsSvc.Float("eval.stddevK"), MinSamples: settingsSvc.Int("eval.minSamples")},
			settingsSvc.Int("eval.windowHours"),
		).WithOnDrift(evalOnDrift(taskBroadcaster))
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
	hookStore := hookstore.New(settingsSvc.Int("hooks.eventsPerSession"), hookstore.DefaultTTL)

	// Spawner attribution. Runs after the pipeline enricher because it reads the
	// PipelineTaskID that one sets; sessions without a task are placed from the
	// config dir their process carries.
	spawnerEnricher := agentbroadcast.NewSpawnerEnricher(spawnerRepo, taskRepoForResolver)

	// Hook enforcer: holds PreToolUse hook calls open so an approval prompt
	// can be answered here instead of in the session's terminal. Built at this
	// point rather than in the router because the enricher below reads the same
	// instance, and the enricher has to exist before the router is constructed.
	hookEnforcer := hooks.NewHookEnforcer(nil)
	// Every config dir, not just the server's own: a session can run under a
	// custom CLAUDE_CONFIG_DIR and its deny rules live there.
	hookEnforcer.SetDenyReader(claudesettings.NewReader(parser.AllClaudeConfigDirs()...))

	// Combine the read-only crossings into one enricher applied at every GetAgents
	// call site. A nil pipelineEnricher (no DB) composes away.
	agentEnricher := merger.ChainEnrichers(
		pipelineEnricher,
		spawnerEnricher,
		agentbroadcast.NewHookEventEnricher(hookStore),
		agentbroadcast.NewPermissionBridgeEnricher(hookEnforcer),
	)

	// Built here (not earlier) so it captures agentEnricher — admin agent search
	// results carry the same pipeline-task and hook-event annotations as /api/agents.
	var searchHandler *search.Handler
	if bundle != nil {
		searchHandler = search.NewHandler(rawrepo.NewSearchRepo(bundle.DB), agentMerger, agentEnricher, routerConfig.BypassAuth)
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
	var grantsHandler *grants.Handler
	if entClient != nil {
		grantsHandler = grants.NewHandler(repo.NewGrantRepo(entClient), repo.NewCapabilityRepo(entClient))
	}
	var systemPromptsHandler *systemprompts.Handler
	if entClient != nil {
		systemPromptsHandler = systemprompts.NewHandler(systemPromptRepo)
	}
	var promptTemplatesHandler *prompttemplates.Handler
	if entClient != nil {
		promptTemplatesHandler = prompttemplates.NewHandler(repo.NewPromptTemplateRepo(entClient))
	}
	adapterHandler := adapters.NewHandler()
	replyStore := agents.NewReplyStore()
	var channelStageOutputHandler *agents.ChannelStageOutputHandler
	var auditEventRepo repo.AuditEventRepo
	if entClient != nil {
		channelStageOutputHandler = agents.NewChannelStageOutputHandler(repo.NewStageRunRepo(entClient), apiKeyRepo)
		auditEventRepo = repo.NewAuditEventRepo(entClient)
	}

	usageHandler := apiusage.NewHandler(settingsSvc, nil) // nil agg = uses default scanner

	var onboardingHandler *onboarding.Handler
	if apiKeyRepo != nil {
		onboardingHandler = onboarding.NewHandler(settingsSvc, apiKeyRepo)
	}

	var trackerHandler *trackerapi.Handler
	if pluginSettingsSvc != nil {
		trackerHandler = trackerapi.NewHandler(pluginSettingsSvc, nil, nil)
	}

	routerDeps := api.RouterDeps{
		Ctx:                    ctx,
		Config:                 routerConfig,
		AgentBroadcaster:       broadcaster,
		Merger:                 agentMerger,
		Enricher:               agentEnricher,
		HookStore:              hookStore,
		HookEnforcer:           hookEnforcer,
		CapabilityDecisions:    capabilityDecisions,
		CapabilityAsker:        capabilityAskerFor(memAsker),
		OAuthProvider:          oauthProvider,
		UserRepo:               userRepo,
		ApiKeyRepo:             apiKeyRepo,
		ProjectRepo:            projectRepo,
		ProjectFolderRepo:      projectFolderRepo,
		SpawnerRepo:            spawnerRepo,
		SpawnerBroadcaster:     spawnerBroadcaster,
		ProjectBroadcaster:     projectBroadcaster,
		TaskProjectOps:         newTaskProjectOps(entClient),
		TaskHandler:            taskHandler,
		CoordHandler:           coordapi.New(repo.NewScratchpadRepo(entClient), repo.NewCoordLockRepo(entClient)),
		WebPushHandler:         webPushHandler,
		RemotesHandler:         remotesHandler,
		PresetsHandler:         presetsHandler,
		PermissionPresetRepo:   permissionPresetRepo,
		GrantsHandler:          grantsHandler,
		SystemPromptsHandler:   systemPromptsHandler,
		PromptTemplatesHandler: promptTemplatesHandler,
		AdapterHandler:         adapterHandler,
		OnboardingHandler:      onboardingHandler,
		ProvidersHandler:       providersHandler,
		SettingsHandler:        settingsHandler,
		SearchHandler:          searchHandler,
		HistoryHandler:         historyHandler,
		MemoryHandler:          memoryHandler,
		RefineHandler:          refineHandler,
		PlanHandler:            planHandler,
		AnalyticsHandler:       analyticsHandler,
		CostHandler:            costHandler,
		EvalHandler:            evalHandler,
		VisualizationsHandler:  apivisualizations.NewHandler(),
		MCPHandler:             mcpHandler,
		SchedulesHandler:       schedulesHandler,
		ChannelReply:           agents.NewChannelReplyHandler(replyStore, apiKeyRepo, repo.NewStageRunRepo(entClient)),
		ChannelStageOutput:     channelStageOutputHandler,
		PluginRegistry:         pluginRegistry,
		PluginLifecycleHandler: pluginLifecycleHandler,
		AuditEventRepo:         auditEventRepo,
		UsageHandler:           usageHandler,
		TrackerHandler:         trackerHandler,
		AdminHandler: admin.New(
			restart.NewAuthProviderValidator(pluginRegistry, activePluginIDs(pluginRepo), cfg.PluginDir),
			string(restartCtl.Mode()),
			restartCtl.Trigger,
		),
	}
	router := api.NewRouter(routerDeps)
	server := provideServer(cfg, settingsSvc, router, ln)
	return &ServerComponents{
		API:                 server,
		Broadcaster:         broadcaster,
		Merger:              agentMerger,
		Orchestrator:        orch,
		Scheduler:           sched,
		HistImporter:        histImporter,
		Baseline:            baselineProvider,
		Enricher:            agentEnricher,
		CapabilityDecisions: capabilityDecisions,
		Eval:                evalService,
		Settings:            settingsSvc,
		Obsidian:            obsidianClient,
		Cleanup:             cleanup,
	}, nil
}

// activePluginIDs returns a closure listing the IDs of plugins currently marked
// active in the DB. Used by the restart validator to predict the next boot's
// auth_provider set. Nil repo (no DB) -> no active plugins.
func activePluginIDs(pluginRepo repo.PluginRepo) func(context.Context) ([]string, error) {
	return func(ctx context.Context) ([]string, error) {
		if pluginRepo == nil {
			return nil, nil
		}
		rows, err := pluginRepo.List(ctx)
		if err != nil {
			return nil, err
		}
		var out []string
		for _, p := range rows {
			if p.Active {
				out = append(out, p.ID)
			}
		}
		return out, nil
	}
}

// askerArgFor avoids assigning a nil *serverask.Asker straight into the interface, which would defeat memory.Gate.Authorize's `Asker == nil` check with a non-nil interface wrapping a nil pointer.
func askerArgFor(asker *serverask.Asker) capability.Asker {
	if asker == nil {
		return nil
	}
	return asker
}

// capabilityAskerFor is askerArgFor for the router's narrower interface, and
// exists for the same reason: the router tests `deps.CapabilityAsker != nil`.
func capabilityAskerFor(asker *serverask.Asker) interface {
	SetOnChange(func())
	Resolve(id, decision string) (serverask.Pending, error)
} {
	if asker == nil {
		return nil
	}
	return asker
}

func toPendingCapabilityDecisions(entries []askgate.Entry[serverask.Pending]) []sdk.PendingCapabilityDecision {
	out := make([]sdk.PendingCapabilityDecision, len(entries))
	for i, e := range entries {
		out[i] = sdk.PendingCapabilityDecision{
			ID:            e.ID,
			Capability:    e.Meta.Capability,
			Value:         e.Meta.Value,
			ValueElided:   e.Meta.ValueElided,
			Context:       e.Meta.Context,
			ContextElided: e.Meta.ContextElided,
			Reason:        e.Meta.Reason,
			RequestedAt:   e.Meta.RequestedAt.UTC().Format(time.RFC3339),
		}
	}
	return out
}
