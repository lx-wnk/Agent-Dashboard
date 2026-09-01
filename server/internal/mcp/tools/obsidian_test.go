package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	obsidianapp "github.com/lx-wnk/agent-dashboard/server/internal/apps/obsidian"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
	"github.com/lx-wnk/agent-dashboard/server/internal/memory"
)

// TestObsidianToolsHaveScopeEntries proves all four Obsidian tools have a
// ToolScopeMap entry — Register panics at construction without one.
func TestObsidianToolsHaveScopeEntries(t *testing.T) {
	for _, name := range []string{"obsidian_read", "obsidian_search", "obsidian_write", "obsidian_delete"} {
		if _, ok := mcp.ToolScopeMap[name]; !ok {
			t.Errorf("%s has no ToolScopeMap entry — Register panics at construction without one", name)
		}
	}
}

// TestObsidianScopesAreGrantableToKeys asserts both new scopes appear in the
// set an API key may be granted directly, mirroring
// TestMemoryScopesAreGrantableToKeys.
func TestObsidianScopesAreGrantableToKeys(t *testing.T) {
	require.True(t, validKeyScopes["obsidian:read"], "obsidian:read must be directly grantable to a key")
	require.True(t, validKeyScopes["obsidian:write"], "obsidian:write must be directly grantable to a key")
}

// TestKeysManageImpliesObsidianScopes asserts the expansion of keys:manage
// contains both obsidian scopes explicitly — scopeImplies is one level
// deep, not transitive, so keys:manage's own entry must list them by hand.
func TestKeysManageImpliesObsidianScopes(t *testing.T) {
	resolved := mcp.ResolveScopes([]string{"keys:manage"})
	require.True(t, resolved["obsidian:read"], "keys:manage must imply obsidian:read")
	require.True(t, resolved["obsidian:write"], "keys:manage must imply obsidian:write")
}

// TestObsidianWriteImpliesObsidianRead asserts scopeImplies directly for the
// obsidian:write -> obsidian:read edge, not just through keys:manage's
// aggregate expansion above. A dropped row here would leave every
// write-only key unable to call obsidian_read, uncaught by the keys:manage
// test alone.
func TestObsidianWriteImpliesObsidianRead(t *testing.T) {
	resolved := mcp.ResolveScopes([]string{"obsidian:write"})
	require.True(t, resolved["obsidian:read"], "obsidian:write must imply obsidian:read")
}

// newFakeObsidianVault serves a minimal Obsidian Local REST API — one note
// under "root/a.md" — and records whether it was ever contacted, mirroring
// internal/api/obsidian/handler_test.go's newFakeVault. A test uses the
// returned bool to prove the gate denied a call before the vault client
// ever made a request.
func newFakeObsidianVault(t *testing.T) (*httptest.Server, *bool) {
	t.Helper()
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("/search/simple/", func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{"filename": "root/a.md", "score": 1}})
	})
	mux.HandleFunc("/vault/", func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello from the vault"))
	})
	ts := httptest.NewTLSServer(mux)
	t.Cleanup(ts.Close)
	return ts, &called
}

func newTestObsidianClient(t *testing.T, ts *httptest.Server) *obsidianapp.Client {
	t.Helper()
	client, err := obsidianapp.NewClient(obsidianapp.Config{
		BaseURL:   "https://" + ts.Listener.Addr().String(),
		APIKey:    "secret",
		VaultRoot: "root",
		TLSMode:   obsidianapp.TLSPinned,
	})
	require.NoError(t, err)
	return client
}

// newObsidianDepsForTest wires ObsidianDeps against an in-memory database
// and a fake vault server that records whether it was reached. Capabilities
// are deliberately left unseeded — no repo.SeedCapabilities and no
// obsidianapp.Register call — mirroring newMemoryDepsForTest, so a test can
// exercise the "capability never catalogued, therefore denied" path
// explicitly. The Gate carries no Asker, same as newMemoryDepsForTest's.
func newObsidianDepsForTest(t *testing.T) (ObsidianDeps, repo.GrantRepo, repo.CapabilityRepo, context.Context, *bool) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	capabilities := repo.NewCapabilityRepo(bundle.Client)
	grants := repo.NewGrantRepo(bundle.Client)

	ts, called := newFakeObsidianVault(t)
	deps := ObsidianDeps{
		Client: newTestObsidianClient(t, ts),
		Gate: memory.Gate{
			Capabilities: capabilities,
			Grants:       grants,
			GrantUsage:   repo.NewGrantUsageRepo(bundle.Client, bundle.WriteClient),
		},
	}
	return deps, grants, capabilities, context.Background(), called
}

// obsidianTestDepsWithCatalogue mirrors internal/api/obsidian/handler_test.go's
// testDeps: it runs obsidianapp.Register so the four obsidian.* capabilities
// are catalogued as class "reach" (rather than left unseeded), letting a
// test exercise the "catalogued, no grant" ask path distinctly from
// newObsidianDepsForTest's "never catalogued" deny path.
func obsidianTestDepsWithCatalogue(t *testing.T) (ObsidianDeps, repo.GrantRepo, context.Context, *bool) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	capabilities := repo.NewCapabilityRepo(bundle.Client)
	resources := repo.NewResourceRepo(bundle.Client)
	ctx := context.Background()
	require.NoError(t, obsidianapp.Register(ctx, resources, capabilities))

	grants := repo.NewGrantRepo(bundle.Client)
	ts, called := newFakeObsidianVault(t)
	deps := ObsidianDeps{
		Client: newTestObsidianClient(t, ts),
		// No Asker: proves the ask-effect path fails closed
		// (capability.ErrAskRequired) at the unit-test level, the same way
		// memory's equivalent test does. Production wiring (di_mcp.go)
		// carries a real Asker instead — see ObsidianDeps' own doc comment.
		Gate: memory.Gate{Capabilities: capabilities, Grants: grants, GrantUsage: repo.NewGrantUsageRepo(bundle.Client, bundle.WriteClient)},
	}
	return deps, grants, ctx, called
}

// TestObsidianDeleteDeniedBeforeVaultCall is the regression test decision D8
// exists for: obsidian.delete is never catalogued here (repo.SeedCapabilities
// does not seed it — only Claude Code tool names and the memory
// capabilities), so the zero-value CapabilityView's empty Class resolves to
// deny before the client is ever touched.
func TestObsidianDeleteDeniedBeforeVaultCall(t *testing.T) {
	deps, _, capRepo, ctx, vaultCalled := newObsidianDepsForTest(t)
	repo.SeedCapabilities(ctx, capRepo)
	registry := mcp.ToolRegistry{}
	RegisterObsidianTools(registry, deps)

	_, err := registry["obsidian_delete"].Handler(ctx, map[string]any{"path": "notes/a.md"})
	require.Error(t, err)
	// Pins the deny path specifically: if obsidian.delete were ever added to
	// SeedCapabilities, this would silently flip to the ask path
	// (capability.ErrAskRequired) while require.Error alone stayed green.
	require.ErrorContains(t, err, "capability denied")
	assert.False(t, *vaultCalled, "the vault must not be touched before the gate allows it")
}

// TestObsidianToolsDenyBeforeVaultCallWhenCapabilityUncatalogued extends the
// same proof to obsidian_read, obsidian_search and obsidian_write — write
// and delete are the two operations decision D8 calls out as irreversible,
// so both must be shown, not just delete.
func TestObsidianToolsDenyBeforeVaultCallWhenCapabilityUncatalogued(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"obsidian_read", map[string]any{"path": "notes/a.md"}},
		{"obsidian_search", map[string]any{"query": "anything"}},
		{"obsidian_write", map[string]any{"path": "notes/a.md", "content": "hello"}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			deps, _, capRepo, ctx, vaultCalled := newObsidianDepsForTest(t)
			repo.SeedCapabilities(ctx, capRepo)
			registry := mcp.ToolRegistry{}
			RegisterObsidianTools(registry, deps)

			_, err := registry[tc.tool].Handler(ctx, tc.args)
			require.Error(t, err)
			require.ErrorContains(t, err, tc.tool)
			// Same pin as TestObsidianDeleteDeniedBeforeVaultCall: without
			// this, a future SeedCapabilities change could silently swap
			// deny for ask here and every assertion above would stay green.
			require.ErrorContains(t, err, "capability denied")
			assert.False(t, *vaultCalled, "the vault must not be touched before the gate allows it")
		})
	}
}

// TestObsidianWriteDeniedWithoutGrantEvenWhenCapabilityCatalogued proves the
// second sentinel this task's brief calls out: with obsidian.write
// catalogued (class "reach", via obsidianapp.Register) but no grant at all,
// Decide has no candidate and defaultEffect("reach") resolves to "ask" — and
// with no Asker wired, ServerEnforcer fails closed with
// capability.ErrAskRequired, not capability.ErrDenied. This is the ask path;
// TestObsidianDeleteDeniedBeforeVaultCall above is the deny path — both must
// be reachable and both must be covered.
func TestObsidianWriteDeniedWithoutGrantEvenWhenCapabilityCatalogued(t *testing.T) {
	deps, _, ctx, called := obsidianTestDepsWithCatalogue(t)
	registry := mcp.ToolRegistry{}
	RegisterObsidianTools(registry, deps)

	_, err := registry["obsidian_write"].Handler(ctx, map[string]any{"path": "notes/a.md", "content": "hi"})
	require.Error(t, err)
	require.ErrorContains(t, err, "obsidian_write")
	require.ErrorContains(t, err, "no asker is configured",
		"a catalogued reach-class capability with no grant resolves to ask, not deny — this must be capability.ErrAskRequired, not ErrDenied")
	assert.False(t, *called, "the vault must not be touched while the ask is unanswerable")
}

// TestObsidianToolsNotRegisteredWhenClientNil pins the nil-client decision:
// an unconfigured vault means the four tools are never registered at all,
// rather than registered and answering a fixed "not configured" error every
// time — an agent discovering a tool it can never use is worse than not
// discovering it.
func TestObsidianToolsNotRegisteredWhenClientNil(t *testing.T) {
	registry := mcp.ToolRegistry{}
	RegisterObsidianTools(registry, ObsidianDeps{Client: nil})

	for _, name := range []string{"obsidian_read", "obsidian_search", "obsidian_write", "obsidian_delete"} {
		_, ok := registry[name]
		assert.False(t, ok, "%s must not be registered when the vault client is nil", name)
	}
}

func TestObsidianReadSucceedsWithAnAllowGrant(t *testing.T) {
	deps, grants, ctx, called := obsidianTestDepsWithCatalogue(t)
	mustAllowMemoryGrant(t, grants, ctx, obsidianapp.CapabilityRead)

	registry := mcp.ToolRegistry{}
	RegisterObsidianTools(registry, deps)

	out := invokeCoordTool(t, registry, ctx, "obsidian_read", map[string]any{"path": "a.md"})
	assert.Equal(t, "hello from the vault", out["content"])
	assert.True(t, *called, "an allowed read must reach the vault")
}

func TestObsidianSearchSucceedsWithAnAllowGrant(t *testing.T) {
	deps, grants, ctx, called := obsidianTestDepsWithCatalogue(t)
	mustAllowMemoryGrant(t, grants, ctx, obsidianapp.CapabilitySearch)

	registry := mcp.ToolRegistry{}
	RegisterObsidianTools(registry, deps)

	out := invokeCoordTool(t, registry, ctx, "obsidian_search", map[string]any{"query": "anything"})
	results, ok := out["results"].([]any)
	require.True(t, ok, "results must be a list, got %T", out["results"])
	require.Len(t, results, 1)
	assert.True(t, *called, "an allowed search must reach the vault")
}

// TestObsidianWriteSucceedsWithAnAllowGrant is the D8 write half of this
// task: obsidian.write is class "reach" and Reversible:false — a grant here
// is a human deliberately authorizing a destructive action, not a default.
func TestObsidianWriteSucceedsWithAnAllowGrant(t *testing.T) {
	deps, grants, ctx, called := obsidianTestDepsWithCatalogue(t)
	mustAllowMemoryGrant(t, grants, ctx, obsidianapp.CapabilityWrite)

	registry := mcp.ToolRegistry{}
	RegisterObsidianTools(registry, deps)

	out := invokeCoordTool(t, registry, ctx, "obsidian_write", map[string]any{"path": "a.md", "content": "hello"})
	assert.Equal(t, "a.md", out["path"])
	assert.True(t, *called, "an allowed write must reach the vault")
}

// TestObsidianDeleteSucceedsWithAnAllowGrant is the D8 delete half: the
// gate this whole task exists to prove is exercised against the one
// operation that cannot be undone.
func TestObsidianDeleteSucceedsWithAnAllowGrant(t *testing.T) {
	deps, grants, ctx, called := obsidianTestDepsWithCatalogue(t)
	mustAllowMemoryGrant(t, grants, ctx, obsidianapp.CapabilityDelete)

	registry := mcp.ToolRegistry{}
	RegisterObsidianTools(registry, deps)

	out := invokeCoordTool(t, registry, ctx, "obsidian_delete", map[string]any{"path": "a.md"})
	assert.Equal(t, "a.md", out["path"])
	assert.Equal(t, true, out["deleted"])
	assert.True(t, *called, "an allowed delete must reach the vault")
}

// TestObsidianToolsRefuseTraversalEscapingAPatternNarrowedGrant is the
// regression test for fix round 1's CRITICAL finding: the gate and the
// vault client must agree on the same target. Client.Read/Write/Delete
// normalize notePath through resolveVaultPath before building a request,
// but capability.Match (pattern.go) is a plain strings.HasPrefix over
// whatever string it is handed — so a raw, un-normalized
// "notes/../secrets/keys.md" passes a "notes/*" grant on the strength of
// its literal prefix, then resolves to "secrets/keys.md" once it reaches
// the client, a target the grant never covered. Client.NormalizeNotePath is
// the fix: every handler below normalizes once and hands the SAME string to
// both Authorize and the client, so what the gate approves is exactly what
// the vault receives.
func TestObsidianToolsRefuseTraversalEscapingAPatternNarrowedGrant(t *testing.T) {
	cases := []struct {
		tool string
		cap  string
		args map[string]any
	}{
		{"obsidian_read", obsidianapp.CapabilityRead, map[string]any{"path": "notes/../secrets/keys.md"}},
		{"obsidian_write", obsidianapp.CapabilityWrite, map[string]any{"path": "notes/../secrets/keys.md", "content": "pwned"}},
		{"obsidian_delete", obsidianapp.CapabilityDelete, map[string]any{"path": "notes/../secrets/keys.md"}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			deps, grants, ctx, called := obsidianTestDepsWithCatalogue(t)
			_, err := grants.Create(ctx, repo.CreateGrantInput{
				CapabilityName: tc.cap,
				Context:        repo.GrantContextFor(repo.GrantContextGlobal, ""),
				Pattern:        "notes/*",
				Mode:           repo.GrantModeAllow,
				GrantedBy:      "test",
			})
			require.NoError(t, err)

			registry := mcp.ToolRegistry{}
			RegisterObsidianTools(registry, deps)

			_, err = registry[tc.tool].Handler(ctx, tc.args)
			require.Error(t, err)
			// The grant exists for this capability but its "notes/*"
			// pattern never matches the normalized "secrets/keys.md", so
			// Decide has no matching candidate and falls to
			// defaultEffect("reach") = ask; with no Asker wired here that
			// surfaces as capability.ErrAskRequired, not ErrDenied — the
			// same two-sentinel distinction TestObsidianWriteDeniedWithoutGrantEvenWhenCapabilityCatalogued
			// covers. Which sentinel fires is not the property under test;
			// that the vault is never touched is.
			require.ErrorContains(t, err, tc.tool)
			assert.False(t, *called, "a path that only starts with the grant's prefix before traversal is collapsed must never reach the vault")
		})
	}
}

// TestObsidianDeleteAllowsATraversalThatStillLandsInTheGrantedSubtree
// proves the fix does not simply reject every ".." — a request whose
// normalized form still falls inside the granted pattern must succeed, the
// same way TestObsidianDeleteSucceedsWithAnAllowGrant already does for the
// no-traversal case. This is what distinguishes "normalize and compare" from
// "reject any path containing .."; the brief was explicit that the former,
// not the latter, is required.
func TestObsidianDeleteAllowsATraversalThatStillLandsInTheGrantedSubtree(t *testing.T) {
	deps, grants, ctx, called := obsidianTestDepsWithCatalogue(t)
	_, err := grants.Create(ctx, repo.CreateGrantInput{
		CapabilityName: obsidianapp.CapabilityDelete,
		Context:        repo.GrantContextFor(repo.GrantContextGlobal, ""),
		Pattern:        "notes/*",
		Mode:           repo.GrantModeAllow,
		GrantedBy:      "test",
	})
	require.NoError(t, err)

	registry := mcp.ToolRegistry{}
	RegisterObsidianTools(registry, deps)

	out := invokeCoordTool(t, registry, ctx, "obsidian_delete", map[string]any{"path": "notes/sub/../a.md"})
	assert.Equal(t, "notes/a.md", out["path"], "the normalized path, not the raw one, is what the tool reports and acts on")
	assert.True(t, *called, "a traversal that still resolves inside the granted subtree must reach the vault")
}
