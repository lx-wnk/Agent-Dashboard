package api

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/lx-wnk/agent-dashboard/server/frontend"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/agents"
	apiauth "github.com/lx-wnk/agent-dashboard/server/internal/api/auth"
	apikeyhandler "github.com/lx-wnk/agent-dashboard/server/internal/api/apikeys"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/hooks"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/memory"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/presets"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/remotes"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/sessions"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/system"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
	authpkg "github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

// RouterConfig holds configuration values for the router.
type RouterConfig struct {
	JWTSecret         string
	CallbackURL       string
	IsLoopback        bool            // true when Host is 127.0.0.1 / ::1 / localhost
	Embedded          http.FileSystem // Vue SPA embed (unused until Task 14)
	HooksSecret       string
	HooksDebounceMs   int
	SpawnRateLimit    int
	SpawnRateWindowMs int
}

// RouterDeps holds all dependencies injected into the router.
type RouterDeps struct {
	Config           RouterConfig
	AgentBroadcaster *sse.Broadcaster
	GitHubClient     *authpkg.GitHubClient
	UserRepo         repo.UserRepo
	ApiKeyRepo       repo.ApiKeyRepo
	TaskHandler      *tasks.Handler
	RemotesHandler   *remotes.Handler
	PresetsHandler   *presets.Handler
	MCPHandler       http.Handler
	ChannelReply     *agents.ChannelReplyHandler
}

// NewRouter builds the chi router with all middleware and route mounts.
func NewRouter(deps RouterDeps) http.Handler {
	r := chi.NewRouter()

	// Global middleware (applied to every request)
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(SlogMiddleware)
	r.Use(chimiddleware.Recoverer)
	r.Use(SecurityHeaders)

	// Public routes (no auth)
	r.Get("/api/system/health", system.HealthHandler)

	// Hooks endpoint: protected by shared secret only (no JWT), exempt from session auth.
	debounceMs := deps.Config.HooksDebounceMs
	if debounceMs <= 0 {
		debounceMs = 100
	}
	hooksHandler := hooks.New(deps.Config.HooksSecret, newDebouncedRescan(deps.AgentBroadcaster, debounceMs))
	r.Post("/api/hooks/event", hooksHandler.Event)
	r.Post("/api/hooks/pre-tool", hooksHandler.PreTool)
	r.Post("/api/hooks/respond", hooksHandler.Respond)
	r.Get("/api/hooks/pending", hooksHandler.Pending)

	// Auth routes (public — OAuth dance must be unauthenticated)
	authHandler := apiauth.NewHandler(apiauth.Deps{
		JWTSecret:    deps.Config.JWTSecret,
		CallbackURL:  deps.Config.CallbackURL,
		GitHubClient: deps.GitHubClient,
		UserRepo:     deps.UserRepo,
		IsLoopback:   deps.Config.IsLoopback,
	})
	r.Get("/api/auth/github", ErrorMiddleware(authHandler.GitHubRedirect))
	r.Get("/api/auth/callback", ErrorMiddleware(authHandler.Callback))
	r.Post("/api/auth/logout", ErrorMiddleware(authHandler.Logout))

	// Protected routes (JWT required)
	r.Group(func(r chi.Router) {
		r.Use(authpkg.RequireAuth(deps.Config.JWTSecret))
		agentHandler := agents.NewHandler(merger.GetAgents, deps.AgentBroadcaster)
		r.Get("/api/agents", ErrorMiddleware(agentHandler.List))
		r.Get("/api/agents/stream", agentHandler.Stream)
		r.Get("/api/agents/{sessionId}/output", sessions.Output)

		r.Get("/api/sessions", sessions.List)
		r.Get("/api/sessions/{sessionId}/timeline", sessions.Timeline)

		r.Get("/api/quota", system.Quota)
		r.Get("/api/system/config", system.Config)
		r.Get("/api/system/system", system.System)

		r.Get("/api/memory", memory.List)
		r.Get("/api/memory/*", memory.Get)
		r.Put("/api/memory/*", memory.Put)

		r.Get("/api/auth/me", ErrorMiddleware(authHandler.Me))

		if deps.ApiKeyRepo != nil {
			apiKeyHandler := apikeyhandler.NewHandler(deps.ApiKeyRepo)
			r.Get("/api/settings/api-keys", ErrorMiddleware(apiKeyHandler.List))
			r.Post("/api/settings/api-keys", ErrorMiddleware(apiKeyHandler.Create))
			r.Delete("/api/settings/api-keys/{id}", ErrorMiddleware(apiKeyHandler.Delete))
		}

		if deps.TaskHandler != nil {
			deps.TaskHandler.Mount(r)
		}

		if deps.RemotesHandler != nil {
			deps.RemotesHandler.Mount(r)
		}

		if deps.PresetsHandler != nil {
			deps.PresetsHandler.Mount(r)
		}
	})

	// Channel-reply endpoint — bearer token auth via discovery file (no JWT).
	// The channel bridge posts here; auth is validated against the per-PID discovery file.
	if deps.ChannelReply != nil {
		r.Post("/api/channel-reply", deps.ChannelReply.Post)
		r.Get("/api/agents/{pid}/replies", deps.ChannelReply.GetReplies)
	}

	// Spawn management — rate-limited user-initiated agent spawning and channel message forwarding.
	spawnMgr := agents.NewSpawnManager(deps.Config.SpawnRateLimit, deps.Config.SpawnRateWindowMs)
	spawnHandler := agents.NewSpawnHandler(spawnMgr)
	r.Post("/api/agents/spawn", spawnHandler.Spawn)
	r.Get("/api/agents/spawn/{pid}/status", spawnHandler.Status)
	r.Post("/api/agents/{pid}/message", spawnHandler.Message)

	// MCP endpoint — Bearer token auth (API key), not JWT session auth.
	// Mounted outside the JWT group so OAuth-less clients can reach it.
	if deps.MCPHandler != nil {
		r.With(mcp.McpAuthMiddleware(deps.ApiKeyRepo)).Post("/api/mcp", deps.MCPHandler.ServeHTTP)
	}

	// Vue SPA catch-all — must be last (after all API routes)
	sub, err := fs.Sub(frontend.Embedded, "dist")
	if err != nil {
		panic("frontend embed sub: " + err.Error())
	}
	r.Handle("/*", NewSPAHandler(sub))

	return r
}

// newDebouncedRescan returns an OnEventFn that triggers an agent rescan after debounceMs.
// Multiple calls within the window collapse into one rescan.
func newDebouncedRescan(broadcaster *sse.Broadcaster, debounceMs int) hooks.OnEventFn {
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
			agents, err := merger.GetAgents(context.Background())
			if err != nil {
				slog.Warn("hooks: debounced rescan failed", "err", err)
				return
			}
			data, err := json.Marshal(agents)
			if err != nil {
				slog.Warn("hooks: marshal failed", "err", err)
				return
			}
			broadcaster.Broadcast(data)
		})
	}
}
