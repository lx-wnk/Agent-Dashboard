package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
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
	r.Get("/api/system/health", func(w http.ResponseWriter, r *http.Request) {
		_ = encode(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Protected routes (JWT required)
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(deps.Config.JWTSecret))
		// Agent routes and SPA handler are mounted in later tasks
	})

	return r
}
