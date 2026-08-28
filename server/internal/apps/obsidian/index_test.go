package obsidian_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/apps/obsidian"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// fakeVault is a minimal stand-in for Obsidian's Local REST API: a mutable
// set of vault-relative note paths to content. /search/simple/ reports every
// note it holds, unfiltered — the real endpoint searches the whole vault,
// not just Client's configured VaultRoot, and IndexNotes has to cope with
// that. /vault/{path} 404s once a path is removed, imitating a note deleted
// out from under an existing pointer.
type fakeVault struct {
	mu    sync.Mutex
	notes map[string]string
	calls int
}

func newFakeVault(notes map[string]string) (*httptest.Server, *fakeVault) {
	fv := &fakeVault{notes: notes}
	mux := http.NewServeMux()
	mux.HandleFunc("/search/simple/", func(w http.ResponseWriter, r *http.Request) {
		fv.mu.Lock()
		defer fv.mu.Unlock()
		fv.calls++
		type item struct {
			Filename string  `json:"filename"`
			Score    float64 `json:"score"`
		}
		items := make([]item, 0, len(fv.notes))
		for path := range fv.notes {
			items = append(items, item{Filename: path, Score: 1})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(items)
	})
	mux.HandleFunc("/vault/", func(w http.ResponseWriter, r *http.Request) {
		fv.mu.Lock()
		defer fv.mu.Unlock()
		fv.calls++
		path := strings.TrimPrefix(r.URL.Path, "/vault/")
		content, ok := fv.notes[path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	})
	return httptest.NewTLSServer(mux), fv
}

func (fv *fakeVault) remove(path string) {
	fv.mu.Lock()
	defer fv.mu.Unlock()
	delete(fv.notes, path)
}

func (fv *fakeVault) callCount() int {
	fv.mu.Lock()
	defer fv.mu.Unlock()
	return fv.calls
}

func newTestClient(t *testing.T, ts *httptest.Server, vaultRoot string) *obsidian.Client {
	t.Helper()
	client, err := obsidian.NewClient(obsidian.Config{
		BaseURL:   "https://" + ts.Listener.Addr().String(),
		APIKey:    "secret",
		VaultRoot: vaultRoot,
		TLSMode:   obsidian.TLSPinned,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

// newIndexTestDeps wires the repos IndexNotes needs against a real
// in-memory SQLite database, with the capability catalogue seeded — the
// same shape memory/authorize_test.go uses, extended with the MemoryRepo
// IndexNotes writes through.
func newIndexTestDeps(t *testing.T) (repo.MemoryRepo, repo.CapabilityRepo, repo.GrantRepo, repo.GrantUsageRepo, context.Context) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })

	capRepo := repo.NewCapabilityRepo(bundle.Client)
	repo.SeedCapabilities(context.Background(), capRepo)
	mem := repo.NewMemoryRepo(bundle.Client, bundle.WriteClient)
	return mem, capRepo, repo.NewGrantRepo(bundle.Client), repo.NewGrantUsageRepo(bundle.Client, bundle.WriteClient), context.Background()
}

func grantMemoryWrite(t *testing.T, ctx context.Context, grants repo.GrantRepo) {
	t.Helper()
	grantCapability(t, ctx, grants, repo.CapabilityMemoryWrite)
}

func grantObsidianSearch(t *testing.T, ctx context.Context, grants repo.GrantRepo) {
	t.Helper()
	grantCapability(t, ctx, grants, obsidian.CapabilitySearch)
}

func grantObsidianRead(t *testing.T, ctx context.Context, grants repo.GrantRepo) {
	t.Helper()
	grantCapability(t, ctx, grants, obsidian.CapabilityRead)
}

func grantCapability(t *testing.T, ctx context.Context, grants repo.GrantRepo, capName string) {
	t.Helper()
	if _, err := grants.Create(ctx, repo.CreateGrantInput{
		CapabilityName: capName,
		Context:        repo.GrantContextFor(repo.GrantContextGlobal, ""),
		Pattern:        "",
		Mode:           repo.GrantModeAllow,
		GrantedBy:      "test",
	}); err != nil {
		t.Fatalf("grant %s: %v", capName, err)
	}
}

func createTestSpace(t *testing.T, ctx context.Context, mem repo.MemoryRepo) *string {
	t.Helper()
	space, err := mem.CreateSpace(ctx, repo.CreateSpaceInput{Slug: "notes", Name: "Notes", Scope: repo.GlobalScope()})
	if err != nil {
		t.Fatalf("CreateSpace: %v", err)
	}
	return &space.ID
}

func TestIndexWritesPointersNotBodies(t *testing.T) {
	mem, caps, grants, grantUsage, ctx := newIndexTestDeps(t)
	grantMemoryWrite(t, ctx, grants)
	grantObsidianSearch(t, ctx, grants)
	grantObsidianRead(t, ctx, grants)
	spaceID := *createTestSpace(t, ctx, mem)

	body := strings.Repeat("this note has a very long body that must never end up in memory. ", 100)
	ts, _ := newFakeVault(map[string]string{"root/long.md": body})
	defer ts.Close()
	client := newTestClient(t, ts, "root")

	count, err := obsidian.IndexNotes(ctx, client, mem, caps, grants, grantUsage, spaceID)
	if err != nil {
		t.Fatalf("IndexNotes: %v", err)
	}
	if count != 1 {
		t.Fatalf("IndexNotes: want 1 indexed, got %d", count)
	}

	entries, err := mem.ListValid(ctx, spaceID, time.Now())
	if err != nil {
		t.Fatalf("ListValid: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("ListValid: want 1 entry, got %d", len(entries))
	}
	entry := entries[0]
	if strings.Contains(entry.Content, "very long body") {
		t.Fatalf("entry content contains the note body: %q", entry.Content)
	}
	if entry.SourceRef == nil || *entry.SourceRef != "long.md" {
		t.Fatalf("entry source_ref: want %q, got %v", "long.md", entry.SourceRef)
	}
	if entry.Kind != "pointer" {
		t.Fatalf("entry kind: want pointer, got %q", entry.Kind)
	}
	if entry.SourceKind != "application" {
		t.Fatalf("entry source_kind: want application, got %q", entry.SourceKind)
	}
}

func TestIndexRequiresMemoryWriteGrant(t *testing.T) {
	mem, caps, grants, grantUsage, ctx := newIndexTestDeps(t)
	// No memory.write grant created — the application gets no privileged path.
	spaceID := *createTestSpace(t, ctx, mem)

	ts, fv := newFakeVault(map[string]string{"root/a.md": "hello"})
	defer ts.Close()
	client := newTestClient(t, ts, "root")

	count, err := obsidian.IndexNotes(ctx, client, mem, caps, grants, grantUsage, spaceID)
	if err == nil {
		t.Fatal("IndexNotes: want error with no memory.write grant, got nil")
	}
	if count != 0 {
		t.Fatalf("IndexNotes: want 0 indexed, got %d", count)
	}
	// Asserting only on the error would pass an implementation that reaches
	// the vault first and reports the denial afterwards.
	if got := fv.callCount(); got != 0 {
		t.Fatalf("IndexNotes: want the vault never contacted before the grant is checked, got %d calls", got)
	}

	entries, err := mem.ListValid(ctx, spaceID, time.Now())
	if err != nil {
		t.Fatalf("ListValid: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("ListValid: want 0 entries written without a grant, got %d", len(entries))
	}
}

func TestStalePointerIsMarkedInvalidNotDeleted(t *testing.T) {
	mem, caps, grants, grantUsage, ctx := newIndexTestDeps(t)
	grantMemoryWrite(t, ctx, grants)
	grantObsidianSearch(t, ctx, grants)
	grantObsidianRead(t, ctx, grants)
	spaceID := *createTestSpace(t, ctx, mem)

	ts, fv := newFakeVault(map[string]string{"root/a.md": "hello"})
	defer ts.Close()
	client := newTestClient(t, ts, "root")

	if _, err := obsidian.IndexNotes(ctx, client, mem, caps, grants, grantUsage, spaceID); err != nil {
		t.Fatalf("first IndexNotes: %v", err)
	}
	entries, err := mem.ListValid(ctx, spaceID, time.Now())
	if err != nil || len(entries) != 1 {
		t.Fatalf("after first index: want 1 entry, got %d (err %v)", len(entries), err)
	}
	entryID := entries[0].ID

	// The note is deleted from the vault between index and the next access.
	fv.remove("root/a.md")

	if _, err := obsidian.IndexNotes(ctx, client, mem, caps, grants, grantUsage, spaceID); err != nil {
		t.Fatalf("second IndexNotes: %v", err)
	}

	stillValid, err := mem.ListValid(ctx, spaceID, time.Now())
	if err != nil {
		t.Fatalf("ListValid: %v", err)
	}
	for _, e := range stillValid {
		if e.ID == entryID {
			t.Fatal("stale pointer is still valid after its note was deleted from the vault")
		}
	}

	row, err := mem.GetEntry(ctx, entryID)
	if err != nil {
		t.Fatalf("GetEntry: stale pointer must still exist (marked invalid, not deleted): %v", err)
	}
	if row.ID != entryID {
		t.Fatal("GetEntry: returned a different row")
	}
}

func TestIndexRequiresObsidianSearchGrant(t *testing.T) {
	mem, caps, grants, grantUsage, ctx := newIndexTestDeps(t)
	grantMemoryWrite(t, ctx, grants)
	// No obsidian.search grant — the gate the docs claim exists must
	// actually deny, not let the first wired-in caller through.
	spaceID := *createTestSpace(t, ctx, mem)

	ts, fv := newFakeVault(map[string]string{"root/a.md": "hello"})
	defer ts.Close()
	client := newTestClient(t, ts, "root")

	count, err := obsidian.IndexNotes(ctx, client, mem, caps, grants, grantUsage, spaceID)
	if err == nil {
		t.Fatal("IndexNotes: want error with no obsidian.search grant, got nil")
	}
	if count != 0 {
		t.Fatalf("IndexNotes: want 0 indexed, got %d", count)
	}
	if got := fv.callCount(); got != 0 {
		t.Fatalf("IndexNotes: want the vault never contacted before the grant is checked, got %d calls", got)
	}
}

func TestIndexRequiresObsidianReadGrant(t *testing.T) {
	mem, caps, grants, grantUsage, ctx := newIndexTestDeps(t)
	grantMemoryWrite(t, ctx, grants)
	grantObsidianSearch(t, ctx, grants)
	// No obsidian.read grant.
	spaceID := *createTestSpace(t, ctx, mem)

	ts, fv := newFakeVault(map[string]string{"root/a.md": "hello"})
	defer ts.Close()
	client := newTestClient(t, ts, "root")

	count, err := obsidian.IndexNotes(ctx, client, mem, caps, grants, grantUsage, spaceID)
	if err == nil {
		t.Fatal("IndexNotes: want error with no obsidian.read grant, got nil")
	}
	if count != 0 {
		t.Fatalf("IndexNotes: want 0 indexed, got %d", count)
	}
	if got := fv.callCount(); got != 0 {
		t.Fatalf("IndexNotes: want the vault never contacted before the grant is checked, got %d calls", got)
	}
}
