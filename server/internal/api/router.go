package api

import (
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/lx-wnk/agent-dashboard/server/frontend"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/agents"
	apiauth "github.com/lx-wnk/agent-dashboard/server/internal/api/auth"
	apikeyhandler "github.com/lx-wnk/agent-dashboard/server/internal/api/apikeys"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/system"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
	authpkg "github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

// RouterConfig holds configuration values for the router.
type RouterConfig struct {
	JWTSecret   string
	CallbackURL string
	IsLoopback  bool            // true when Host is 127.0.0.1 / ::1 / localhost
	Embedded    http.FileSystem // Vue SPA embed (unused until Task 14)
}

// RouterDeps holds all dependencies injected into the router.
type RouterDeps struct {
	Config           RouterConfig
	AgentBroadcaster *sse.Broadcaster
	GitHubClient     *authpkg.GitHubClient
	UserRepo         repo.UserRepo
	ApiKeyRepo       repo.ApiKeyRepo
	TaskHandler      *tasks.Handler
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
	})

	// Vue SPA catch-all — must be last (after all API routes)
	sub, err := fs.Sub(frontend.Embedded, "dist")
	if err != nil {
		panic("frontend embed sub: " + err.Error())
	}
	r.Handle("/*", NewSPAHandler(sub))

	return r
}
