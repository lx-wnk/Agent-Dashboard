package api

import (
	"context"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	apiplugins "github.com/lx-wnk/agent-dashboard/server/internal/api/plugins"
	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

// stubPluginController satisfies the unexported controller interface consumed by
// apiplugins.NewLifecycle. It exists only so the plugin lifecycle routes mount in
// the test router — route registration is static and never drives these methods.
type stubPluginController struct{}

func (stubPluginController) List(context.Context) ([]apiplugins.PluginView, error) { return nil, nil }
func (stubPluginController) Transition(context.Context, string, string) (apiplugins.PluginView, error) {
	return apiplugins.PluginView{}, nil
}
func (stubPluginController) GetSettings(context.Context, string) ([]plugin.SettingField, map[string]string, error) {
	return nil, nil, nil
}
func (stubPluginController) PutSettings(context.Context, string, map[string]string) error { return nil }

var updateGolden = flag.Bool("update-golden", false, "rewrite the route golden fixture")

// collectRoutes walks the fully-wired production router and returns every
// registered route as a sorted "METHOD PATH" line. It is the behavior-preserving
// tripwire for structural refactors (plugin domain consolidation): the route
// table must be byte-identical before and after a package move.
func collectRoutes(t *testing.T) []string {
	t.Helper()
	router := buildBypassRouter(t)
	routes, ok := router.(chi.Routes)
	if !ok {
		t.Fatalf("NewRouter did not return a chi.Routes; got %T", router)
	}

	var lines []string
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		lines = append(lines, method+" "+route)
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	sort.Strings(lines)
	return lines
}

// TestRouteGolden asserts the production route set matches the checked-in golden
// fixture. Run with -update-golden to regenerate after an intentional route change.
func TestRouteGolden(t *testing.T) {
	got := strings.Join(collectRoutes(t), "\n") + "\n"
	goldenPath := filepath.Join("testdata", "routes.golden")

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update-golden to create): %v", err)
	}
	if got != string(want) {
		t.Errorf("route set drifted from golden.\nRegenerate intentionally with:\n  go test ./internal/api -run TestRouteGolden -update-golden\n\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}
