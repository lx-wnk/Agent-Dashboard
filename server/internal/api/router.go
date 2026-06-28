package api

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/frontend"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/adapters"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/agents"
	apianalytics "github.com/lx-wnk/agent-dashboard/server/internal/api/analytics"
	apikeyhandler "github.com/lx-wnk/agent-dashboard/server/internal/api/apikeys"
	apiauth "github.com/lx-wnk/agent-dashboard/server/internal/api/auth"
	apiconfig "github.com/lx-wnk/agent-dashboard/server/internal/api/config"
	coordapi "github.com/lx-wnk/agent-dashboard/server/internal/api/coord"
	apicost "github.com/lx-wnk/agent-dashboard/server/internal/api/cost"
	apieval "github.com/lx-wnk/agent-dashboard/server/internal/api/eval"
	apihistory "github.com/lx-wnk/agent-dashboard/server/internal/api/history"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/hooks"
	planapi "github.com/lx-wnk/agent-dashboard/server/internal/api/plan"
	apiplugins "github.com/lx-wnk/agent-dashboard/server/internal/api/plugins"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/presets"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/projects"
	providersapi "github.com/lx-wnk/agent-dashboard/server/internal/api/providers"
	refineapi "github.com/lx-wnk/agent-dashboard/server/internal/api/refine"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/remotes"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/schedules"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/search"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/sessions"
	settingsapi "github.com/lx-wnk/agent-dashboard/server/internal/api/settings"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/spawners"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/system"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/systemprompts"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/visualizations"
	apiwp "github.com/lx-wnk/agent-dashboard/server/internal/api/wphandler"
	authpkg "github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/hookstore"
	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
	"github.com/lx-wnk/agent-dashboard/server/internal/services"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

// newAgentsAccessor builds the request-scoped GetAgents accessor expected by the
// read handlers (agents/sessions/config/search). The return type is an unnamed
// func so it stays assignable to each handler's own named accessor type
// (agents.GetAgentsFn, cmdscope.AgentsFn, …).
//
// These HTTP read paths do not compute the health-score cost baseline — they pass
// zero-value baseline opts, so the cost component is neutral (no penalty); the
// broadcast loop is the single place that injects a real baseline. The
// pipeline-task enricher, by contrast, IS applied here so request-scoped reads
// carry the same PipelineTaskID/Title annotation as the SSE stream. A nil
// enricher disables it.
func newAgentsAccessor(m *merger.Merger, enricher merger.Enricher) func(ctx context.Context) ([]sdk.Agent, error) {
	return func(ctx context.Context) ([]sdk.Agent, error) {
		return m.GetAgents(ctx, merger.GetAgentsOpts{Enricher: enricher})
	}
}

// RouterConfig holds configuration values for the router.
type RouterConfig struct {
	JWTSecret          string
	CallbackURL        string
	IsLoopback         bool            // true when Host is 127.0.0.1 / ::1 / localhost
	BypassAuth         bool            // skip JWT when DASHBOARD_AUTH=none
	Embedded           http.FileSystem // Vue SPA embed (unused until Task 14)
	HooksSecret        string
	HooksDebounceMs    int
	SpawnRateLimit     int
	SpawnRateWindowMs  int
	InjectRateLimit    int
	InjectRateWindowMs int
	// AuthPluginSecret is forwarded to the auth handler to protect POST /api/auth/session.
	AuthPluginSecret string
	// PluginLoginURL, when non-empty, causes GET /api/auth/login to redirect to the
	// auth plugin instead of handling the OAuth dance in core.
	PluginLoginURL string
	// LoopbackHostConfig configures the DNS-rebinding protection middleware.
	// The zero value applies the default loopback whitelist (127.0.0.1, localhost, ::1).
	LoopbackHostConfig RequireLoopbackHostConfig
	// AuthRateLimiterConfig configures the per-IP rate limiter applied to auth,
	// MCP, and bulk-resolve endpoints. The zero value uses safe defaults (10 r/s, burst 20).
	AuthRateLimiterConfig IPRateLimiterConfig
}

// RouterDeps holds all dependencies injected into the router.
type RouterDeps struct {
	// Ctx is the server-lifetime context. When cancelled (e.g. on shutdown) any
	// background goroutines started by the router (e.g. debounced rescan) are
	// also cancelled. If nil, context.Background() is used as a fallback.
	Ctx              context.Context
	Config           RouterConfig
	AgentBroadcaster *sse.Broadcaster
	// Merger is the shared roster builder (single instance per process, owns the
	// cross-tick stale tracker). Used by every request-scoped GetAgents accessor.
	Merger *merger.Merger
	// Enricher, when non-nil, annotates each scanned agent with its linked
	// pipeline task (read-only SQLite crossing). Applied to every request-scoped
	// GetAgents call below via the agentsAccessor closure. May be nil (no DB →
	// no enrichment). The broadcast loop receives the same enricher separately.
	Enricher merger.Enricher
	// HookStore records per-event hook granularity. The same instance is read by
	// the Enricher (via the agentbroadcast hook enricher) so events POSTed to
	// /api/hooks/event surface on the matching agent. May be nil (recording off).
	HookStore         *hookstore.Store
	OAuthProvider     authpkg.OAuthProvider
	UserRepo          repo.UserRepo
	ApiKeyRepo        repo.ApiKeyRepo
	ProjectRepo       repo.ProjectRepo
	ProjectFolderRepo repo.ProjectFolderRepo
	SpawnerRepo       repo.SpawnerRepo
	// SpawnerBroadcaster fans out spawner CRUD events to SSE subscribers.
	// May be nil; Stream is only mounted in DI where a broadcaster is always provided.
	SpawnerBroadcaster *sse.SpawnerBroadcaster
	// ProjectBroadcaster fans out project CRUD events to SSE subscribers.
	// May be nil; Stream is only mounted in DI where a broadcaster is always provided.
	ProjectBroadcaster *sse.ProjectBroadcaster
	// TaskProjectOps lets the projects handler check for active tasks and
	// clear project_id on done/cancelled tasks during DELETE /api/projects/{id}.
	// May be nil; when nil the project handler skips the active-task check.
	TaskProjectOps        projects.TaskProjectOps
	CoordHandler          *coordapi.Handler
	TaskHandler           *tasks.Handler
	SchedulesHandler      *schedules.Handler
	WebPushHandler        *apiwp.Handler
	RemotesHandler        *remotes.Handler
	PresetsHandler        *presets.Handler
	SystemPromptsHandler  *systemprompts.Handler
	SearchHandler         *search.Handler
	HistoryHandler        *apihistory.Handler
	RefineHandler         *refineapi.Handler
	PlanHandler           *planapi.Handler
	AnalyticsHandler      *apianalytics.Handler
	CostHandler           *apicost.Handler
	EvalHandler           *apieval.Handler
	VisualizationsHandler *visualizations.Handler
	AdapterHandler        *adapters.Handler
	ProvidersHandler      *providersapi.Handler
	SettingsHandler       *settingsapi.Handler
	MCPHandler            http.Handler
	ChannelReply          *agents.ChannelReplyHandler
	ChannelStageOutput    *agents.ChannelStageOutputHandler
	PermissionPresetRepo  repo.PermissionPresetRepo
	PluginRegistry         *plugin.Registry
	PluginsHandler         *apiplugins.Handler
	PluginLifecycleHandler *apiplugins.LifecycleHandler
	AuditEventRepo         repo.AuditEventRepo
}

// NewRouter builds the chi router with all middleware and route mounts.
func NewRouter(deps RouterDeps) http.Handler {
	serverCtx := deps.Ctx
	if serverCtx == nil {
		serverCtx = context.Background()
	}

	// Single request-scoped GetAgents accessor, shared by every read path below
	// (SSOT). Captures the injected pipeline-task enricher so HTTP reads and the
	// debounced rescan carry the same task annotation as the SSE broadcast loop.
	getAgents := newAgentsAccessor(deps.Merger, deps.Enricher)

	r := chi.NewRouter()

	// Build the per-IP rate limiter once; it owns its cleanup goroutine.
	// serverCtx cancels the goroutine on shutdown.
	authRateLimiter := NewIPRateLimiter(serverCtx, deps.Config.AuthRateLimiterConfig)

	// Global middleware (applied to every request, including hooks/MCP/channel-reply)
	// StripForwardedHeaders must be FIRST so no downstream middleware ever sees
	// attacker-controlled X-Forwarded-Host / X-Forwarded-Proto / Forwarded values.
	r.Use(StripForwardedHeaders)
	r.Use(chimiddleware.RequestID)
	r.Use(SlogMiddleware)
	r.Use(chimiddleware.Recoverer)
	r.Use(SecurityHeaders)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, 8*1024*1024) // 8 MB
			next.ServeHTTP(w, r)
		})
	})
	r.Use(gzipMiddleware)

	// Public routes (no auth)
	r.Get("/api/system/health", system.HealthHandler)

	// Hook-script ingress: bearer-secret auth, no JWT. These are posted by the
	// local Claude Code hook scripts (which carry DASHBOARD_HOOKS_SECRET), not by
	// the browser, so they are mounted outside the session-auth group.
	debounceMs := deps.Config.HooksDebounceMs
	if debounceMs <= 0 {
		debounceMs = 100
	}
	hooksHandler := hooks.New(deps.Config.HooksSecret, deps.HookStore, newDebouncedRescan(serverCtx, deps.AgentBroadcaster, debounceMs, getAgents))
	r.Post("/api/hooks/event", hooksHandler.Event)
	r.Post("/api/hooks/pre-tool", hooksHandler.PreTool)
	// NOTE: /api/hooks/respond and /api/hooks/pending are browser-facing (the edit
	// gate UI reads pending edits and posts the user's decision). They carry the
	// session cookie, not the hooks secret, so they are registered inside the
	// protected JWT group below — NOT here.

	// Auth routes (public — OAuth dance must be unauthenticated)
	// F-SEC-010: per-IP rate limit prevents auth-probing and SHA-256 amplification DoS.
	authHandler := apiauth.NewHandler(apiauth.Deps{
		JWTSecret:        deps.Config.JWTSecret,
		CallbackURL:      deps.Config.CallbackURL,
		OAuthProvider:    deps.OAuthProvider,
		UserRepo:         deps.UserRepo,
		IsLoopback:       deps.Config.IsLoopback,
		BypassAuth:       deps.Config.BypassAuth,
		AuthPluginSecret: deps.Config.AuthPluginSecret,
		PluginLoginURL:   deps.Config.PluginLoginURL,
	})
	r.With(authRateLimiter).Get("/api/auth/login", ErrorMiddleware(authHandler.LoginRedirect))
	r.With(authRateLimiter).Get("/api/auth/github", ErrorMiddleware(authHandler.LoginRedirect)) // backwards-compat alias
	r.With(authRateLimiter).Get("/api/auth/callback", ErrorMiddleware(authHandler.Callback))
	r.With(authRateLimiter).Post("/api/auth/logout", ErrorMiddleware(authHandler.Logout))
	// Plugin session endpoint — called by external auth plugins after OAuth completes.
	// Only active when DASHBOARD_AUTH_PLUGIN_SECRET is set.
	r.With(authRateLimiter).Post("/api/auth/session", ErrorMiddleware(authHandler.CreateSession))

	// Protected routes (JWT required, unless auth bypass is active)
	r.Group(func(r chi.Router) {
		// F-SEC-005: reject requests whose Host header is not in the loopback
		// whitelist. Scoped to the browser-facing protected group; hooks, MCP,
		// and channel-reply are excluded because they use bearer-token auth and
		// may be called from non-browser clients on the same machine.
		r.Use(RequireLoopbackHost(deps.Config.LoopbackHostConfig))
		// RequireSameOriginForMutations guards against CSRF in both auth modes:
		// in bypass mode it is the primary CSRF defence; in auth mode it is
		// defence-in-depth on top of JWT validation.
		r.Use(RequireSameOriginForMutations)
		// F-SEC-010: per-IP rate limit on all protected endpoints — catches
		// bulk-resolve, permission-request creation, and any other high-cost
		// pipeline paths. 10 r/s burst 20 is well above normal UI usage.
		r.Use(authRateLimiter)
		if !deps.Config.BypassAuth {
			r.Use(authpkg.RequireAuth(deps.Config.JWTSecret))
		}
		agentHandler := agents.NewHandler(getAgents, deps.AgentBroadcaster)
		r.Get("/api/agents", ErrorMiddleware(agentHandler.List))
		r.Get("/api/agents/stream", agentHandler.Stream)
		r.Get("/api/agents/{sessionId}/output", sessions.Output)

		r.Get("/api/sessions", sessions.List)
		r.Get("/api/sessions/{sessionId}/timeline", sessions.Timeline)
		commandsHandler := sessions.NewCommandsHandler(deps.SpawnerRepo, getAgents)
		r.Get("/api/slash-commands", commandsHandler.SlashCommands)

		r.Get("/api/quota", system.Quota)
		r.Get("/api/config", system.Config)        // frontend expects /api/config
		r.Get("/api/system/config", system.Config) // keep old path for compatibility
		r.Get("/api/system", system.System)        // frontend expects /api/system
		r.Get("/api/system/system", system.System) // keep old path for compatibility

		r.Get("/api/me", ErrorMiddleware(authHandler.Me))
		r.Delete("/api/me", ErrorMiddleware(authHandler.DeleteMe))

		if deps.ApiKeyRepo != nil {
			apiKeyHandler := apikeyhandler.NewHandler(deps.ApiKeyRepo)
			r.Get("/api/settings/api-keys", ErrorMiddleware(apiKeyHandler.List))
			r.Post("/api/settings/api-keys", ErrorMiddleware(apiKeyHandler.Create))
			r.Delete("/api/settings/api-keys/{id}", ErrorMiddleware(apiKeyHandler.Delete))
			r.Post("/api/settings/api-keys/{id}/regenerate", ErrorMiddleware(apiKeyHandler.Regenerate))
		}

		if deps.ProvidersHandler != nil {
			r.Get("/api/providers", ErrorMiddleware(deps.ProvidersHandler.List))
			r.Patch("/api/providers/{id}", ErrorMiddleware(deps.ProvidersHandler.Patch))
		}

		if deps.SettingsHandler != nil {
			deps.SettingsHandler.Mount(r)
		}

		// Projects + ProjectFolders — JWT-protected (no admin gate required).
		if deps.ProjectRepo != nil && deps.ProjectFolderRepo != nil {
			projectsHandler := projects.NewHandler(deps.ProjectRepo, deps.ProjectFolderRepo, deps.TaskProjectOps, deps.ProjectBroadcaster)
			projectsHandler.Mount(r)
			r.Get("/api/projects/stream", projectsHandler.Stream)
		}

		// Spawners — JWT + admin-or-bypass: spawner CRUD lets the operator define
		// arbitrary processes, so it is RCE-equivalent and must be admin-only.
		if deps.SpawnerRepo != nil {
			r.Group(func(r chi.Router) {
				r.Use(authpkg.RequireAdminOrBypass(deps.Config.BypassAuth))
				spawnersHandler := spawners.NewHandler(deps.SpawnerRepo, deps.SpawnerBroadcaster)
				spawnersHandler.Mount(r)
			})
			// Read-only live stream — JWT-protected but not admin-gated.
			streamHandler := spawners.NewHandler(deps.SpawnerRepo, deps.SpawnerBroadcaster)
			r.Get("/api/spawners/stream", streamHandler.Stream)
		}

		if deps.WebPushHandler != nil {
			deps.WebPushHandler.Mount(r)
		}

		if deps.TaskHandler != nil {
			deps.TaskHandler.Mount(r)
		}

		if deps.CoordHandler != nil {
			deps.CoordHandler.Mount(r)
		}

		if deps.SchedulesHandler != nil {
			deps.SchedulesHandler.Mount(r)
		}

		if deps.RemotesHandler != nil {
			deps.RemotesHandler.Mount(r)
		}

		if deps.PresetsHandler != nil {
			deps.PresetsHandler.Mount(r)
		}

		if deps.SystemPromptsHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(authpkg.RequireAdminOrBypass(deps.Config.BypassAuth))
				deps.SystemPromptsHandler.Mount(r)
			})
		}

		if deps.SearchHandler != nil {
			r.Get("/api/search", ErrorMiddleware(deps.SearchHandler.Search))
		}

		if deps.HistoryHandler != nil {
			deps.HistoryHandler.Mount(r)
		}

		if deps.RefineHandler != nil {
			deps.RefineHandler.Mount(r)
		}

		if deps.PlanHandler != nil {
			deps.PlanHandler.Mount(r)
		}

		if deps.AnalyticsHandler != nil {
			deps.AnalyticsHandler.Mount(r)
		}

		if deps.VisualizationsHandler != nil {
			deps.VisualizationsHandler.Mount(r)
		}

		if deps.AdapterHandler != nil {
			deps.AdapterHandler.Mount(r)
		}

		// Cost analytics — aggregated spend by model, day, and week.
		if deps.CostHandler != nil {
			deps.CostHandler.Mount(r)
		}

		// Eval metrics and drift alerts.
		if deps.EvalHandler != nil {
			deps.EvalHandler.Mount(r)
		}

		// Spawn management — rate-limited user-initiated agent spawning and channel message forwarding.
		// Inside the protected group so only authenticated users can spawn agents.
		//
		// Build the cwd allow-list from registered project folder paths (F-SEC-001).
		// Sensitive home dirs (~/.ssh, ~/.aws, etc.) are always blocked regardless.
		var spawnPolicy services.SpawnPolicy
		if deps.ProjectRepo != nil && deps.ProjectFolderRepo != nil {
			spawnPolicy = services.NewSpawnPolicy(services.ProjectFolderRootsProvider(deps.ProjectRepo, deps.ProjectFolderRepo))
		} else {
			spawnPolicy = services.NewSpawnPolicy(nil)
		}
		spawnMgr := agents.NewSpawnManager(
			deps.Config.SpawnRateLimit, deps.Config.SpawnRateWindowMs,
			deps.Config.InjectRateLimit, deps.Config.InjectRateWindowMs,
			deps.SpawnerRepo, spawnPolicy,
		)
		spawnMgr.SetProjectFolderRepo(deps.ProjectFolderRepo)
		go spawnMgr.StartPruner(serverCtx)
		spawnHandler := agents.NewSpawnHandler(spawnMgr)
		if deps.AuditEventRepo != nil {
			spawnHandler.SetAuditRepo(deps.AuditEventRepo)
		}
		if deps.Merger != nil {
			spawnHandler.SetAgentDismisser(deps.Merger)
		}
		r.Post("/api/agents/spawn", spawnHandler.Spawn)
		r.Get("/api/agents/spawn/{pid}/status", spawnHandler.Status)
		r.Post("/api/agents/{pid}/message", spawnHandler.Message)
		r.Delete("/api/agents/{pid}/channel", spawnHandler.DismissChannel)
		if deps.PermissionPresetRepo != nil {
			allowToolHandler := agents.NewAllowToolHandler(getAgents, deps.PermissionPresetRepo)
			r.Post("/api/agents/{pid}/allow-tool", ErrorMiddleware(allowToolHandler.AllowTool))
		}

		// Config explorer — enumerate and edit skills, slash commands, and memory
		// files, scoped per spawner / live session via ?spawnerId / ?sessionId.
		// The only client path accepted is ?cwd; it is sanitized and confined to
		// the spawn policy's project roots so the editable set stays bounded.
		configHandler := apiconfig.NewHandler(deps.SpawnerRepo, getAgents, spawnPolicy)
		r.Get("/api/config/skills", configHandler.Skills)
		r.Get("/api/config/commands", configHandler.Commands)
		r.Get("/api/config/memory", configHandler.Memory)
		// Single-file read/write for editable (user/project) config files. Writes
		// are authorized only against the scope's enumerated editable set.
		r.Get("/api/config/file", configHandler.File)
		r.Put("/api/config/file", configHandler.SaveFile)

		// Edit-gate UI endpoints — browser-facing, session-authenticated (or bypass).
		// Unlike /api/hooks/event and /api/hooks/pre-tool (hook-script ingress, secret
		// auth), these are called by EditGateModal.vue with the session cookie.
		r.Get("/api/hooks/pending", hooksHandler.Pending)
		r.Post("/api/hooks/respond", hooksHandler.Respond)

		if deps.PluginsHandler != nil {
			deps.PluginsHandler.Mount(r)
		}
		// SP1 lifecycle + settings endpoints under the clean /api/plugins namespace.
		// Admin-gated: install/activate/deactivate/uninstall/settings are operator
		// actions equivalent in risk to spawner CRUD.
		if deps.PluginLifecycleHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(authpkg.RequireAdminOrBypass(deps.Config.BypassAuth))
				deps.PluginLifecycleHandler.Mount(r)
			})
		}
		// Live route/ui-extension dispatch. One catch-all resolves the registry per
		// request: chi freezes routes after serve (chi #480), so enable/disable
		// cannot mutate the route tree. Mounted inside the authed group so it
		// inherits JWT + same-origin guards; the proxy strips Cookie/Authorization
		// before forwarding to the plugin.
		if deps.PluginRegistry != nil {
			r.Handle("/api/plugins/{id}/proxy/*", plugin.NewDispatcher(deps.PluginRegistry))
		}
	})

	// Channel-reply endpoint — bearer token auth via discovery file (no JWT).
	// The channel bridge posts here; auth is validated against the per-PID discovery file.
	if deps.ChannelReply != nil {
		r.Post("/api/channel-reply", deps.ChannelReply.Post)
		r.Get("/api/agents/{sessionId}/replies", deps.ChannelReply.GetReplies)
	}

	// Channel-stage-output endpoint — bearer token auth via api_keys (MCP token),
	// no JWT/Origin/loopback middleware — server-to-server call from the bridge.
	if deps.ChannelStageOutput != nil {
		r.Post("/api/channel-stage-output", deps.ChannelStageOutput.Post)
	}

	// Agent-ingress permission-request creation — bearer token auth via api_keys
	// (MCP token), no JWT/Origin/loopback middleware: server-to-server call from
	// the channel bridge. Resolution endpoints stay in the protected group above.
	if deps.TaskHandler != nil && deps.ApiKeyRepo != nil {
		r.Group(func(r chi.Router) {
			r.Use(authRateLimiter)
			r.Use(mcp.McpAuthMiddleware(deps.ApiKeyRepo))
			deps.TaskHandler.MountAgentIngress(r)
		})
	}

	// MCP endpoint — Bearer token auth (API key), not JWT session auth.
	// F-SEC-010: per-IP rate limit prevents SHA-256 amplification DoS on the
	// API-key lookup path. Applied alongside the existing auth middleware.
	// Mounted outside the JWT group so OAuth-less clients can reach it.
	if deps.MCPHandler != nil {
		r.With(authRateLimiter, mcp.McpAuthMiddleware(deps.ApiKeyRepo)).Post(mcp.EndpointPath, deps.MCPHandler.ServeHTTP)
	}

	// Vue SPA catch-all — must be last (after all API routes)
	sub, err := fs.Sub(frontend.Embedded, "dist")
	if err != nil {
		panic("frontend embed sub: " + err.Error())
	}
	r.Handle("/*", NewSPAHandler(sub))

	return r
}

// gzipPool reuses gzip.Writer instances across requests to avoid the ~32 KiB
// per-response allocation that gzip.NewWriterLevel would otherwise incur.
var gzipPool = sync.Pool{
	New: func() any {
		gz, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
		return gz
	},
}

// gzipResponseWriter wraps http.ResponseWriter to write through a gzip.Writer.
type gzipResponseWriter struct {
	http.ResponseWriter
	Writer *gzip.Writer
}

var _ http.Flusher = (*gzipResponseWriter)(nil)

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	return g.Writer.Write(b)
}

func (g *gzipResponseWriter) Flush() {
	_ = g.Writer.Flush()
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// gzipHijackWriter extends gzipResponseWriter to also implement http.Hijacker.
// Used when the underlying ResponseWriter supports connection hijacking (e.g. WebSocket upgrades).
type gzipHijackWriter struct {
	*gzipResponseWriter
	http.Hijacker
}

var _ http.Hijacker = (*gzipHijackWriter)(nil)
var _ http.Flusher = (*gzipHijackWriter)(nil)

// newGzipWriter wraps w with gzip compression. If w also implements http.Hijacker,
// the returned writer forwards Hijack calls to the underlying writer so chi middleware
// and WebSocket upgrade paths continue to work correctly.
func newGzipWriter(w http.ResponseWriter, gz *gzip.Writer) http.ResponseWriter {
	base := &gzipResponseWriter{ResponseWriter: w, Writer: gz}
	if h, ok := w.(http.Hijacker); ok {
		return &gzipHijackWriter{gzipResponseWriter: base, Hijacker: h}
	}
	return base
}

// gzipMiddleware compresses non-SSE responses when the client accepts gzip encoding.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set Vary unconditionally so shared caches know this URL varies by
		// Accept-Encoding, even for responses served without compression (RFC 7234).
		// Use Add rather than Set to preserve any Vary values already set by handlers.
		w.Header().Add("Vary", "Accept-Encoding")

		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		// Skip compression for SSE streams to avoid buffering issues.
		if r.Header.Get("Accept") == "text/event-stream" {
			next.ServeHTTP(w, r)
			return
		}
		gz := gzipPool.Get().(*gzip.Writer)
		gz.Reset(w)
		defer func() {
			_ = gz.Close()
			gzipPool.Put(gz)
		}()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length")
		next.ServeHTTP(newGzipWriter(w, gz), r)
	})
}

// newDebouncedRescan returns an OnEventFn that triggers an agent rescan after debounceMs.
// Multiple calls within the window collapse into one rescan.
// ctx should be the server-lifetime context so the rescan is cancelled on shutdown.
func newDebouncedRescan(ctx context.Context, broadcaster *sse.Broadcaster, debounceMs int, getAgents func(context.Context) ([]sdk.Agent, error)) hooks.OnEventFn {
	var mu sync.Mutex
	var timer *time.Timer
	delay := time.Duration(debounceMs) * time.Millisecond

	return func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(delay, func() {
			// Respect server shutdown — skip rescan if context is done.
			if ctx.Err() != nil {
				return
			}
			agents, err := getAgents(ctx)
			if err != nil {
				slog.Warn("hooks: debounced rescan failed", "err", err)
				return
			}
			data, err := json.Marshal(map[string]any{"agents": agents, "trend": []any{}})
			if err != nil {
				slog.Warn("hooks: marshal failed", "err", err)
				return
			}
			broadcaster.Broadcast(data)
		})
	}
}
