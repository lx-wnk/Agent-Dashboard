package api

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/lx-wnk/agent-dashboard/server/frontend"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/agents"
	apianalytics "github.com/lx-wnk/agent-dashboard/server/internal/api/analytics"
	apiauth "github.com/lx-wnk/agent-dashboard/server/internal/api/auth"
	apikeyhandler "github.com/lx-wnk/agent-dashboard/server/internal/api/apikeys"
	apihistory "github.com/lx-wnk/agent-dashboard/server/internal/api/history"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/hooks"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/memory"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/presets"
	refineapi "github.com/lx-wnk/agent-dashboard/server/internal/api/refine"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/remotes"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/search"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/sessions"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/system"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
	apiwp "github.com/lx-wnk/agent-dashboard/server/internal/api/wphandler"
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
	BypassAuth        bool            // skip JWT when loopback + no GitHub OAuth configured
	Embedded          http.FileSystem // Vue SPA embed (unused until Task 14)
	HooksSecret       string
	HooksDebounceMs   int
	SpawnRateLimit    int
	SpawnRateWindowMs int
}

// RouterDeps holds all dependencies injected into the router.
type RouterDeps struct {
	// Ctx is the server-lifetime context. When cancelled (e.g. on shutdown) any
	// background goroutines started by the router (e.g. debounced rescan) are
	// also cancelled. If nil, context.Background() is used as a fallback.
	Ctx              context.Context
	Config           RouterConfig
	AgentBroadcaster *sse.Broadcaster
	OAuthProvider    authpkg.OAuthProvider
	UserRepo         repo.UserRepo
	ApiKeyRepo       repo.ApiKeyRepo
	TaskHandler      *tasks.Handler
	WebPushHandler   *apiwp.Handler
	RemotesHandler   *remotes.Handler
	PresetsHandler   *presets.Handler
	SearchHandler    *search.Handler
	HistoryHandler   *apihistory.Handler
	RefineHandler    *refineapi.Handler
	AnalyticsHandler *apianalytics.Handler
	MCPHandler       http.Handler
	ChannelReply     *agents.ChannelReplyHandler
}

// NewRouter builds the chi router with all middleware and route mounts.
func NewRouter(deps RouterDeps) http.Handler {
	serverCtx := deps.Ctx
	if serverCtx == nil {
		serverCtx = context.Background()
	}

	r := chi.NewRouter()

	// Global middleware (applied to every request)
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
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

	// Hooks endpoint: protected by shared secret only (no JWT), exempt from session auth.
	debounceMs := deps.Config.HooksDebounceMs
	if debounceMs <= 0 {
		debounceMs = 100
	}
	hooksHandler := hooks.New(deps.Config.HooksSecret, newDebouncedRescan(serverCtx, deps.AgentBroadcaster, debounceMs))
	r.Post("/api/hooks/event", hooksHandler.Event)
	r.Post("/api/hooks/pre-tool", hooksHandler.PreTool)
	r.Post("/api/hooks/respond", hooksHandler.Respond)
	r.Get("/api/hooks/pending", hooksHandler.Pending)

	// Auth routes (public — OAuth dance must be unauthenticated)
	authHandler := apiauth.NewHandler(apiauth.Deps{
		JWTSecret:    deps.Config.JWTSecret,
		CallbackURL:  deps.Config.CallbackURL,
		OAuthProvider: deps.OAuthProvider,
		UserRepo:     deps.UserRepo,
		IsLoopback:   deps.Config.IsLoopback,
		BypassAuth:   deps.Config.BypassAuth,
	})
	r.Get("/api/auth/github", ErrorMiddleware(authHandler.GitHubRedirect))
	r.Get("/api/auth/callback", ErrorMiddleware(authHandler.Callback))
	r.Post("/api/auth/logout", ErrorMiddleware(authHandler.Logout))

	// Protected routes (JWT required, unless auth bypass is active)
	r.Group(func(r chi.Router) {
		// RequireSameOriginForMutations guards against CSRF in both auth modes:
		// in bypass mode it is the primary CSRF defence; in auth mode it is
		// defence-in-depth on top of JWT validation.
		r.Use(RequireSameOriginForMutations)
		if !deps.Config.BypassAuth {
			r.Use(authpkg.RequireAuth(deps.Config.JWTSecret))
		}
		agentHandler := agents.NewHandler(merger.GetAgents, deps.AgentBroadcaster)
		r.Get("/api/agents", ErrorMiddleware(agentHandler.List))
		r.Get("/api/agents/stream", agentHandler.Stream)
		r.Get("/api/agents/{sessionId}/output", sessions.Output)

		r.Get("/api/sessions", sessions.List)
		r.Get("/api/sessions/{sessionId}/timeline", sessions.Timeline)
		r.Get("/api/slash-commands", sessions.SlashCommands)

		r.Get("/api/quota", system.Quota)
		r.Get("/api/config", system.Config)         // frontend expects /api/config
		r.Get("/api/system/config", system.Config)  // keep old path for compatibility
		r.Get("/api/system", system.System)         // frontend expects /api/system
		r.Get("/api/system/system", system.System)  // keep old path for compatibility

		r.Get("/api/memory", memory.List)
		r.Get("/api/memory/*", memory.Get)
		r.Put("/api/memory/*", memory.Put)

		r.Get("/api/me", ErrorMiddleware(authHandler.Me))
		r.Delete("/api/me", ErrorMiddleware(authHandler.DeleteMe))

		if deps.ApiKeyRepo != nil {
			apiKeyHandler := apikeyhandler.NewHandler(deps.ApiKeyRepo)
			r.Get("/api/settings/api-keys", ErrorMiddleware(apiKeyHandler.List))
			r.Post("/api/settings/api-keys", ErrorMiddleware(apiKeyHandler.Create))
			r.Delete("/api/settings/api-keys/{id}", ErrorMiddleware(apiKeyHandler.Delete))
		}

		if deps.WebPushHandler != nil {
			deps.WebPushHandler.Mount(r)
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

		if deps.SearchHandler != nil {
			r.Get("/api/search", ErrorMiddleware(deps.SearchHandler.Search))
		}

		if deps.HistoryHandler != nil {
			deps.HistoryHandler.Mount(r)
		}

		if deps.RefineHandler != nil {
			deps.RefineHandler.Mount(r)
		}

		if deps.AnalyticsHandler != nil {
			deps.AnalyticsHandler.Mount(r)
		}

		// Spawn management — rate-limited user-initiated agent spawning and channel message forwarding.
		// Inside the protected group so only authenticated users can spawn agents.
		spawnMgr := agents.NewSpawnManager(deps.Config.SpawnRateLimit, deps.Config.SpawnRateWindowMs)
		spawnHandler := agents.NewSpawnHandler(spawnMgr)
		r.Post("/api/agents/spawn", spawnHandler.Spawn)
		r.Get("/api/agents/spawn/{pid}/status", spawnHandler.Status)
		r.Post("/api/agents/{pid}/message", spawnHandler.Message)
	})

	// Channel-reply endpoint — bearer token auth via discovery file (no JWT).
	// The channel bridge posts here; auth is validated against the per-PID discovery file.
	if deps.ChannelReply != nil {
		r.Post("/api/channel-reply", deps.ChannelReply.Post)
		r.Get("/api/agents/{pid}/replies", deps.ChannelReply.GetReplies)
	}

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
		gz, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		defer gz.Close()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length")
		next.ServeHTTP(newGzipWriter(w, gz), r)
	})
}

// newDebouncedRescan returns an OnEventFn that triggers an agent rescan after debounceMs.
// Multiple calls within the window collapse into one rescan.
// ctx should be the server-lifetime context so the rescan is cancelled on shutdown.
func newDebouncedRescan(ctx context.Context, broadcaster *sse.Broadcaster, debounceMs int) hooks.OnEventFn {
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
			agents, err := merger.GetAgents(ctx)
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
