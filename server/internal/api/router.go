package api

import (
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/lx-wnk/agent-dashboard/server/frontend"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/agents"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/system"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

// RouterConfig holds configuration values for the router.
type RouterConfig struct {
	JWTSecret string
	Embedded  http.FileSystem // Vue SPA embed (unused until Task 14)
}

// RouterDeps holds all dependencies injected into the router.
type RouterDeps struct {
	Config           RouterConfig
	AgentBroadcaster *sse.Broadcaster
}

// NewRouter builds the chi router with all middleware and route mounts.
// Phase 1: agent monitoring routes only; additional routes added in later phases.
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

	// Protected routes (JWT required)
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(deps.Config.JWTSecret))
		agentHandler := agents.NewHandler(merger.GetAgents, deps.AgentBroadcaster)
		r.Get("/api/agents", ErrorMiddleware(agentHandler.List))
		r.Get("/api/agents/stream", agentHandler.Stream)
	})

	// Vue SPA catch-all — must be last (after all API routes)
	sub, err := fs.Sub(frontend.Embedded, "dist")
	if err != nil {
		panic("frontend embed sub: " + err.Error())
	}
	r.Handle("/*", NewSPAHandler(sub))

	return r
}
