package memory_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	apimemory "github.com/lx-wnk/agent-dashboard/server/internal/api/memory"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/memory"
)

// newMux wires an apimemory.Handler against a real in-memory SQLite database
// and returns the mux plus the raw dependencies tests need to seed fixtures.
func newMux(t *testing.T) (*chi.Mux, *ent.Client, repo.MemoryRepo, repo.GrantRepo, repo.CapabilityRepo, context.Context) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	memRepo := repo.NewMemoryRepo(bundle.Client)
	capRepo := repo.NewCapabilityRepo(bundle.Client)
	grantRepo := repo.NewGrantRepo(bundle.Client)
	h := apimemory.NewHandler(memRepo, memory.NewRetriever(bundle.DB, memRepo), capRepo, grantRepo)

	mux := chi.NewRouter()
	h.Mount(mux)
	return mux, bundle.Client, memRepo, grantRepo, capRepo, context.Background()
}

// mustAllowGrant creates an allow grant for capName at the given context,
// wildcard pattern, so it resolves EffectAllow regardless of the value a
// caller passes.
func mustAllowGrant(t *testing.T, grants repo.GrantRepo, ctx context.Context, capName, contextKind, contextRef string) {
	t.Helper()
	_, err := grants.Create(ctx, repo.CreateGrantInput{
		CapabilityName: capName,
		Context:        repo.GrantContextFor(contextKind, contextRef),
		Pattern:        "",
		Mode:           repo.GrantModeAllow,
		GrantedBy:      "test",
	})
	require.NoError(t, err)
}

// mustStageRun creates a task and a stage run against client, for tests
// exercising GET /api/memory/injections?stageRun= — RecordInjection refuses
// a stage_run_id that does not reference a real row.
func mustStageRun(t *testing.T, client *ent.Client) string {
	t.Helper()
	ctx := context.Background()
	task, err := repo.NewTaskRepo(client).Create(ctx, repo.CreateTaskInput{
		Slug: "memory-http-task", Title: "Test Task", Cwd: "/tmp",
		CurrentStage: "concept", Priority: "medium", MaxIterations: 20, StageTimeoutSeconds: 1800,
	})
	require.NoError(t, err)
	run, err := repo.NewStageRunRepo(client).Create(ctx, repo.CreateStageRunInput{TaskID: task.ID, Stage: "concept", Iteration: 1})
	require.NoError(t, err)
	return run.ID
}

func doJSON(t *testing.T, mux *chi.Mux, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func decodeMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}

func decodeSlice(t *testing.T, w *httptest.ResponseRecorder) []any {
	t.Helper()
	var out []any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}

func TestListSpacesDeniedWithoutGrant(t *testing.T) {
	mux, _, _, _, capRepo, ctx := newMux(t)
	repo.SeedCapabilities(ctx, capRepo)

	w := doJSON(t, mux, http.MethodGet, "/api/memory/spaces", nil)
	require.Equal(t, http.StatusForbidden, w.Code)
}

// TestListSpacesDeniedWhenCapabilityNotSeeded proves the catalogue-miss path
// fails closed too: with SeedCapabilities never having run, memory.read has
// no row, and Decide's defaultEffect("") denies.
func TestListSpacesDeniedWhenCapabilityNotSeeded(t *testing.T) {
	mux, _, _, _, _, _ := newMux(t)

	w := doJSON(t, mux, http.MethodGet, "/api/memory/spaces", nil)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestListSpacesFailsClosedOnUnparseableScope(t *testing.T) {
	mux, _, _, _, capRepo, ctx := newMux(t)
	repo.SeedCapabilities(ctx, capRepo)

	w := doJSON(t, mux, http.MethodGet, "/api/memory/spaces?scope=bogus", nil)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateSpaceThenListSpaces(t *testing.T) {
	mux, _, _, grants, capRepo, ctx := newMux(t)
	repo.SeedCapabilities(ctx, capRepo)
	mustAllowGrant(t, grants, ctx, repo.CapabilityMemoryWrite, repo.GrantContextGlobal, "")
	mustAllowGrant(t, grants, ctx, repo.CapabilityMemoryRead, repo.GrantContextGlobal, "")

	w := doJSON(t, mux, http.MethodPost, "/api/memory/spaces", map[string]any{"slug": "notes", "name": "Notes"})
	require.Equal(t, http.StatusCreated, w.Code)
	require.Equal(t, "notes", decodeMap(t, w)["slug"])

	w = doJSON(t, mux, http.MethodGet, "/api/memory/spaces", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, decodeSlice(t, w), 1)
}

func TestCreateSpaceFailsClosedOnUnparseableScope(t *testing.T) {
	mux, _, _, grants, capRepo, ctx := newMux(t)
	repo.SeedCapabilities(ctx, capRepo)
	mustAllowGrant(t, grants, ctx, repo.CapabilityMemoryWrite, repo.GrantContextGlobal, "")

	w := doJSON(t, mux, http.MethodPost, "/api/memory/spaces", map[string]any{"slug": "notes", "scope": "bogus"})
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateEntryFailsClosedOnUnknownSpace(t *testing.T) {
	mux, _, _, grants, capRepo, ctx := newMux(t)
	repo.SeedCapabilities(ctx, capRepo)
	mustAllowGrant(t, grants, ctx, repo.CapabilityMemoryWrite, repo.GrantContextGlobal, "")

	w := doJSON(t, mux, http.MethodPost, "/api/memory/entries", map[string]any{
		"spaceSlug": "does-not-exist", "summary": "s", "content": "c", "kind": "fact", "sourceKind": "user",
	})
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestCreateEntryThenSearchFindsIt(t *testing.T) {
	mux, _, memRepo, grants, capRepo, ctx := newMux(t)
	repo.SeedCapabilities(ctx, capRepo)
	mustAllowGrant(t, grants, ctx, repo.CapabilityMemoryWrite, repo.GrantContextGlobal, "")
	mustAllowGrant(t, grants, ctx, repo.CapabilityMemoryRead, repo.GrantContextGlobal, "")
	_, err := memRepo.CreateSpace(ctx, repo.CreateSpaceInput{Slug: "notes", Name: "Notes", Scope: repo.GlobalScope()})
	require.NoError(t, err)

	w := doJSON(t, mux, http.MethodPost, "/api/memory/entries", map[string]any{
		"spaceSlug": "notes", "summary": "migration runbook", "content": "how to run a migration",
		"kind": "fact", "sourceKind": "user",
	})
	require.Equal(t, http.StatusCreated, w.Code)

	w = doJSON(t, mux, http.MethodGet, "/api/memory/entries?q=migration", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, decodeSlice(t, w), 1)
}

func TestSearchEntriesFailsClosedOnUnparseableScope(t *testing.T) {
	mux, _, _, _, capRepo, ctx := newMux(t)
	repo.SeedCapabilities(ctx, capRepo)

	w := doJSON(t, mux, http.MethodGet, "/api/memory/entries?q=x&scope=bogus", nil)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestSearchEntriesClampsAbsurdLimit proves a zero, negative or enormous
// limit can never turn into an unbounded response — Retriever.Retrieve
// clamps whatever the query string parses to.
func TestSearchEntriesClampsAbsurdLimit(t *testing.T) {
	mux, _, memRepo, grants, capRepo, ctx := newMux(t)
	repo.SeedCapabilities(ctx, capRepo)
	mustAllowGrant(t, grants, ctx, repo.CapabilityMemoryRead, repo.GrantContextGlobal, "")
	space, err := memRepo.CreateSpace(ctx, repo.CreateSpaceInput{Slug: "notes", Name: "Notes", Scope: repo.GlobalScope()})
	require.NoError(t, err)
	for i := 0; i < 60; i++ {
		_, err := memRepo.CreateEntry(ctx, repo.CreateEntryInput{
			SpaceID: space.ID, Summary: fmt.Sprintf("widget entry %d", i), Content: "widget content",
			Kind: "fact", SourceKind: "user", Confidence: 1,
		})
		require.NoError(t, err)
	}

	for _, limit := range []string{"0", "-5", "999999", "not-a-number"} {
		w := doJSON(t, mux, http.MethodGet, "/api/memory/entries?q=widget&limit="+limit, nil)
		require.Equal(t, http.StatusOK, w.Code)
		entries := decodeSlice(t, w)
		require.LessOrEqualf(t, len(entries), 50, "limit=%s must never return an unbounded result set", limit)
	}
}

// TestSecondScopesEntriesNeverAppear proves the row scoping the search and
// list-spaces queries carry: a project scope's own space and entries must
// never surface a same-slug space or entry that belongs to a different
// project scope, even though both are visible to an identical global grant.
func TestSecondScopesEntriesNeverAppear(t *testing.T) {
	mux, _, memRepo, grants, capRepo, ctx := newMux(t)
	repo.SeedCapabilities(ctx, capRepo)
	mustAllowGrant(t, grants, ctx, repo.CapabilityMemoryRead, repo.GrantContextGlobal, "")
	mustAllowGrant(t, grants, ctx, repo.CapabilityMemoryWrite, repo.GrantContextGlobal, "")

	spaceA, err := memRepo.CreateSpace(ctx, repo.CreateSpaceInput{Slug: "notes", Name: "Notes", Scope: repo.ProjectScope("/path/A")})
	require.NoError(t, err)
	_, err = memRepo.CreateEntry(ctx, repo.CreateEntryInput{
		SpaceID: spaceA.ID, Summary: "alpha secret rollout plan", Content: "alpha only detail",
		Kind: "fact", SourceKind: "user", Confidence: 1,
	})
	require.NoError(t, err)

	spaceB, err := memRepo.CreateSpace(ctx, repo.CreateSpaceInput{Slug: "notes", Name: "Notes", Scope: repo.ProjectScope("/path/B")})
	require.NoError(t, err)
	_, err = memRepo.CreateEntry(ctx, repo.CreateEntryInput{
		SpaceID: spaceB.ID, Summary: "bravo secret rollout plan", Content: "bravo only detail",
		Kind: "fact", SourceKind: "user", Confidence: 1,
	})
	require.NoError(t, err)

	w := doJSON(t, mux, http.MethodGet, "/api/memory/entries?q=secret&scope=project&scopeRef=/path/B", nil)
	require.Equal(t, http.StatusOK, w.Code)
	entries := decodeSlice(t, w)
	require.Len(t, entries, 1, "search scoped to B must not also surface A's entry")
	require.Equal(t, "bravo secret rollout plan", entries[0].(map[string]any)["Summary"])

	w = doJSON(t, mux, http.MethodGet, "/api/memory/spaces?scope=project&scopeRef=/path/B", nil)
	require.Equal(t, http.StatusOK, w.Code)
	spaces := decodeSlice(t, w)
	require.Len(t, spaces, 1, "listing B's spaces must not also surface A's same-slug space")
	require.Equal(t, spaceB.ID, spaces[0].(map[string]any)["id"])
}

func TestPatchSupersedeEntry(t *testing.T) {
	mux, _, memRepo, grants, capRepo, ctx := newMux(t)
	repo.SeedCapabilities(ctx, capRepo)
	mustAllowGrant(t, grants, ctx, repo.CapabilityMemoryWrite, repo.GrantContextGlobal, "")
	space, err := memRepo.CreateSpace(ctx, repo.CreateSpaceInput{Slug: "notes", Name: "Notes", Scope: repo.GlobalScope()})
	require.NoError(t, err)
	oldEntry, err := memRepo.CreateEntry(ctx, repo.CreateEntryInput{SpaceID: space.ID, Summary: "old", Content: "old", Kind: "fact", SourceKind: "user", Confidence: 1})
	require.NoError(t, err)
	newEntry, err := memRepo.CreateEntry(ctx, repo.CreateEntryInput{SpaceID: space.ID, Summary: "new", Content: "new", Kind: "fact", SourceKind: "user", Confidence: 1})
	require.NoError(t, err)

	w := doJSON(t, mux, http.MethodPatch, "/api/memory/entries/"+oldEntry.ID, map[string]any{"supersededBy": newEntry.ID})
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, newEntry.ID, decodeMap(t, w)["superseded_by"])
}

func TestPatchSupersedeEntryFailsClosedOnUnknownEntry(t *testing.T) {
	mux, _, _, grants, capRepo, ctx := newMux(t)
	repo.SeedCapabilities(ctx, capRepo)
	mustAllowGrant(t, grants, ctx, repo.CapabilityMemoryWrite, repo.GrantContextGlobal, "")

	w := doJSON(t, mux, http.MethodPatch, "/api/memory/entries/does-not-exist", map[string]any{"supersededBy": "also-missing"})
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteExpiresEntry(t *testing.T) {
	mux, _, memRepo, grants, capRepo, ctx := newMux(t)
	repo.SeedCapabilities(ctx, capRepo)
	mustAllowGrant(t, grants, ctx, repo.CapabilityMemoryWrite, repo.GrantContextGlobal, "")
	space, err := memRepo.CreateSpace(ctx, repo.CreateSpaceInput{Slug: "notes", Name: "Notes", Scope: repo.GlobalScope()})
	require.NoError(t, err)
	entry, err := memRepo.CreateEntry(ctx, repo.CreateEntryInput{SpaceID: space.ID, Summary: "gone soon", Content: "gone soon", Kind: "fact", SourceKind: "user", Confidence: 1})
	require.NoError(t, err)

	w := doJSON(t, mux, http.MethodDelete, "/api/memory/entries/"+entry.ID, nil)
	require.Equal(t, http.StatusNoContent, w.Code)

	valid, err := memRepo.ListValid(ctx, space.ID, time.Now())
	require.NoError(t, err)
	require.Empty(t, valid, "DELETE must expire the entry, not merely acknowledge the request")
}

func TestDeleteEntryDeniedWithoutGrant(t *testing.T) {
	mux, _, memRepo, _, capRepo, ctx := newMux(t)
	repo.SeedCapabilities(ctx, capRepo)
	space, err := memRepo.CreateSpace(ctx, repo.CreateSpaceInput{Slug: "notes", Name: "Notes", Scope: repo.GlobalScope()})
	require.NoError(t, err)
	entry, err := memRepo.CreateEntry(ctx, repo.CreateEntryInput{SpaceID: space.ID, Summary: "s", Content: "c", Kind: "fact", SourceKind: "user", Confidence: 1})
	require.NoError(t, err)

	w := doJSON(t, mux, http.MethodDelete, "/api/memory/entries/"+entry.ID, nil)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestListInjectionsByStageRun(t *testing.T) {
	mux, client, memRepo, grants, capRepo, ctx := newMux(t)
	repo.SeedCapabilities(ctx, capRepo)
	mustAllowGrant(t, grants, ctx, repo.CapabilityMemoryRead, repo.GrantContextGlobal, "")

	stageRunID := mustStageRun(t, client)
	space, err := memRepo.CreateSpace(ctx, repo.CreateSpaceInput{Slug: "notes", Name: "Notes", Scope: repo.GlobalScope()})
	require.NoError(t, err)
	entry, err := memRepo.CreateEntry(ctx, repo.CreateEntryInput{SpaceID: space.ID, Summary: "s", Content: "c", Kind: "fact", SourceKind: "user", Confidence: 1})
	require.NoError(t, err)
	_, err = memRepo.RecordInjection(ctx, repo.RecordInjectionInput{
		StageRunID: stageRunID, EntryIDs: []string{entry.ID}, CharBudget: 4000, CharsUsed: 100, CandidateCount: 1,
	})
	require.NoError(t, err)

	w := doJSON(t, mux, http.MethodGet, "/api/memory/injections?stageRun="+stageRunID, nil)
	require.Equal(t, http.StatusOK, w.Code)
	list := decodeSlice(t, w)
	require.Len(t, list, 1)
	require.Equal(t, stageRunID, list[0].(map[string]any)["stage_run_id"])
}

func TestListInjectionsRequiresStageRunParam(t *testing.T) {
	mux, _, _, _, capRepo, ctx := newMux(t)
	repo.SeedCapabilities(ctx, capRepo)

	w := doJSON(t, mux, http.MethodGet, "/api/memory/injections", nil)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListInjectionsDeniedWithoutGrant(t *testing.T) {
	mux, _, _, _, capRepo, ctx := newMux(t)
	repo.SeedCapabilities(ctx, capRepo)

	w := doJSON(t, mux, http.MethodGet, "/api/memory/injections?stageRun=whatever", nil)
	require.Equal(t, http.StatusForbidden, w.Code)
}
