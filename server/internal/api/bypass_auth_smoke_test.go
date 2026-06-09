package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	apianalytics "github.com/lx-wnk/agent-dashboard/server/internal/api/analytics"
	apicost "github.com/lx-wnk/agent-dashboard/server/internal/api/cost"
	apihistory "github.com/lx-wnk/agent-dashboard/server/internal/api/history"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/presets"
	refineapi "github.com/lx-wnk/agent-dashboard/server/internal/api/refine"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/remotes"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/search"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/systemprompts"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/visualizations"
	apiwp "github.com/lx-wnk/agent-dashboard/server/internal/api/wphandler"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/rawrepo"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	histsvc "github.com/lx-wnk/agent-dashboard/server/internal/history"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	refinesvc "github.com/lx-wnk/agent-dashboard/server/internal/refine"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
	"github.com/lx-wnk/agent-dashboard/server/internal/webpush"
)

// noopOrchestrator satisfies tasks.OrchestratorIface without driving the state machine.
type noopOrchestrator struct{}

func (noopOrchestrator) ProgressTask(_ context.Context, _ string, _ *pipeline.ProgressOpts) (*ent.StageRun, error) {
	return nil, nil
}
func (noopOrchestrator) ResumeFromUser(_ context.Context, _ string) (*ent.StageRun, error) {
	return nil, nil
}
func (noopOrchestrator) NotifyTaskTerminated(_ context.Context, _, _ string) {}
func (noopOrchestrator) InvalidateConfigCache()                              {}

// buildBypassRouter wires the full production router (every handler mounted) in
// bypass-auth mode against an in-memory SQLite database. It mirrors the DI in
// cmd/serve/di.go so the route set matches production exactly — the point of the
// smoke test below is that NO protected route hard-fails with 401/403 when the
// JWT middleware is skipped (DASHBOARD_AUTH=none).
func buildBypassRouter(t *testing.T) http.Handler {
	t.Helper()
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Close() })
	c := bundle.Client
	rawDB := bundle.DB

	// Refine spawner is stubbed so hitting POST /api/refine/{id}/turn never execs a
	// real `claude` process — it returns an immediately-closed stream.
	stubSpawner := func(_ context.Context, _ refinesvc.SpawnConfig, _ *ent.Spawner) (<-chan string, error) {
		ch := make(chan string)
		close(ch)
		return ch, nil
	}

	deps := RouterDeps{
		Ctx: context.Background(),
		Config: RouterConfig{
			BypassAuth:        true,
			IsLoopback:        true,
			HooksSecret:       "test-hooks-secret-please-ignore-32b",
			SpawnRateLimit:    5,
			SpawnRateWindowMs: 60000,
			// Defang the per-IP limiter: the walk fires 100+ requests from one IP
			// in a tight loop, and a 429 would otherwise mask the 401/403 we test for.
			AuthRateLimiterConfig: IPRateLimiterConfig{Rate: rate.Limit(1_000_000), Burst: 1_000_000},
		},
		AgentBroadcaster:  sse.NewBroadcaster(),
		UserRepo:          repo.NewUserRepo(c),
		ApiKeyRepo:        repo.NewApiKeyRepo(c),
		ProjectRepo:       repo.NewProjectRepo(c),
		ProjectFolderRepo: repo.NewProjectFolderRepo(c),
		SpawnerRepo:       repo.NewSpawnerRepo(c),
		TaskHandler: tasks.NewHandler(tasks.Deps{
			Client:            c,
			TaskRepo:          repo.NewTaskRepo(c),
			SRRepo:            repo.NewStageRunRepo(c),
			PermRepo:          repo.NewPermissionRepo(c),
			AuditRepo:         repo.NewAuditEventRepo(c),
			AuditEventRepo:    repo.NewAuditEventRepo(c),
			CfgRepo:           repo.NewPipelineConfigRepo(c),
			DepRepo:           repo.NewDependencyRepo(c),
			ProjectRepo:       repo.NewProjectRepo(c),
			ProjectFolderRepo: repo.NewProjectFolderRepo(c),
			SpawnerRepo:       repo.NewSpawnerRepo(c),
			Orchestrator:      noopOrchestrator{},
			Broadcaster:       sse.NewTaskBroadcaster(sse.NewBroadcaster()),
		}),
		WebPushHandler: apiwp.NewHandler(webpush.NewService(
			rawrepo.NewNotificationConfigRepo(rawDB),
			rawrepo.NewPushSubscriptionRepo(rawDB),
		)),
		RemotesHandler:       remotes.NewHandler(repo.NewRemoteRegistrationRepo(c)),
		PresetsHandler:       presets.NewHandler(repo.NewPermissionPresetRepo(c)),
		SystemPromptsHandler: systemprompts.NewHandler(repo.NewSystemPromptRepo(c)),
		SearchHandler:        search.NewHandler(rawrepo.NewSearchRepo(rawDB)),
		HistoryHandler:       apihistory.NewHandler(histsvc.NewImporter(repo.NewAgentCostTrendRepo(c))),
		RefineHandler: refineapi.NewHandler(refineapi.Deps{
			Turns:     repo.NewRefinementTurnRepo(c),
			Tasks:     repo.NewTaskRepo(c),
			StageRuns: repo.NewStageRunRepo(c),
			Spawner:   stubSpawner,
		}),
		AnalyticsHandler:      apianalytics.NewHandler(rawrepo.NewAnalyticsRepo(rawDB), rawDB, repo.NewPipelineConfigRepo(c)),
		CostHandler:           apicost.NewHandler(rawDB),
		VisualizationsHandler: visualizations.NewHandler(),
	}

	return NewRouter(deps)
}

// bypassSkip reports routes that legitimately do NOT pass through the session-auth
// group in bypass mode and therefore may return 401/403 by design.
func bypassSkip(method, pattern string) bool {
	switch {
	case strings.HasPrefix(pattern, "/api/auth/"): // public OAuth dance (may 302/500)
		return true
	case pattern == "/*": // Vue SPA catch-all, not an API route
		return true
	case pattern == "/api/hooks/event", pattern == "/api/hooks/pre-tool": // hook-script ingress, bearer-secret auth
		return true
	case pattern == "/api/mcp", pattern == "/api/channel-reply", pattern == "/api/channel-stage-output": // bearer-token auth, outside JWT group
		return true
	case strings.HasSuffix(pattern, "/replies"): // channel-reply read, bearer auth
		return true
	case pattern == "/api/agents/stream", pattern == "/api/tasks/stream",
		pattern == "/api/spawners/stream", pattern == "/api/projects/stream": // long-lived SSE
		return true
	case method == http.MethodDelete && pattern == "/api/me": // account deletion disabled in bypass mode (by design)
		return true
	}
	return false
}

// concretePath substitutes chi path params ({id}, {taskId:...}) and wildcards with
// a dummy segment so the route is actually reachable.
func concretePath(pattern string) string {
	parts := strings.Split(pattern, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, "{") || p == "*" {
			parts[i] = "smoke-test"
		}
	}
	return strings.Join(parts, "/")
}

// driveRequest runs one request against h with a hard deadline so streaming /
// blocking handlers can't hang the test. The status code is read only after the
// handler goroutine has returned, so there is no race on the recorder.
func driveRequest(h http.Handler, method, path string) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var body *strings.Reader
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		body = strings.NewReader("")
	} else {
		body = strings.NewReader("{}")
	}
	req := httptest.NewRequest(method, path, body).WithContext(ctx)
	// Loopback host + matching Origin so RequireLoopbackHost and
	// RequireSameOriginForMutations both pass — we are testing auth, not CSRF.
	req.Host = "127.0.0.1"
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		req.Header.Set("Origin", "http://127.0.0.1")
		req.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(rec, req)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		cancel()
		<-done
	}
	return rec.Code
}

// TestBypassAuth_NoProtectedRouteReturnsAuthError is the regression guard for the
// DASHBOARD_AUTH=none bug class: in bypass mode the RequireAuth middleware is
// skipped, so no JWT payload is placed in the request context. Any handler that
// hard-gates on auth.PayloadFromContext (returning 401/403 when it is absent) is
// unreachable in the default config. This test walks EVERY route registered by
// NewRouter and asserts none of them answer an unauthenticated request with 401
// or 403 — except the explicit bypassSkip allow-list (bearer-auth ingress, the
// public OAuth dance, and account deletion which is disabled in bypass by design).
//
// Because it enumerates routes dynamically, any NEW handler added to NewRouter is
// covered automatically — that is the "never again" guarantee.
func TestBypassAuth_NoProtectedRouteReturnsAuthError(t *testing.T) {
	router := buildBypassRouter(t)
	routes, ok := router.(chi.Routes)
	if !ok {
		t.Fatalf("NewRouter did not return a chi.Routes; got %T", router)
	}

	var mu sync.Mutex
	checked := 0

	walkErr := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if bypassSkip(method, route) {
			return nil
		}
		path := concretePath(route)
		code := driveRequest(router, method, path)

		mu.Lock()
		checked++
		mu.Unlock()

		if code == http.StatusUnauthorized || code == http.StatusForbidden {
			t.Errorf("bypass mode: %s %s returned %d — handler must not hard-gate on a JWT payload that bypass never sets (use auth.BypassPayload() on !ok, or drop the guard)", method, route, code)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("chi.Walk: %v", walkErr)
	}

	if checked == 0 {
		t.Fatal("walked zero routes — router wiring is wrong, the guard would be a false pass")
	}
	t.Logf("bypass-auth smoke: %d protected routes checked, none returned 401/403", checked)
}
