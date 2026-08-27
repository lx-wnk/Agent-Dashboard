package repo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func newMemoryRepo(t *testing.T) (repo.MemoryRepo, context.Context) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	return repo.NewMemoryRepo(bundle.Client), context.Background()
}

func mustSpace(t *testing.T, r repo.MemoryRepo, ctx context.Context, slug string) *ent.Resource {
	t.Helper()
	space, err := r.CreateSpace(ctx, repo.CreateSpaceInput{
		Slug:  slug,
		Name:  slug,
		Scope: repo.GlobalScope(),
	})
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	return space
}

func mustEntry(t *testing.T, r repo.MemoryRepo, ctx context.Context, spaceID, summary string) *ent.MemoryEntry {
	t.Helper()
	entry, err := r.CreateEntry(ctx, repo.CreateEntryInput{
		SpaceID:    spaceID,
		Summary:    summary,
		Content:    summary,
		Kind:       "fact",
		SourceKind: "user",
		Confidence: 1,
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	return entry
}

func TestListValidExcludesExpiredAndSuperseded(t *testing.T) {
	r, ctx := newMemoryRepo(t)
	space := mustSpace(t, r, ctx, "project-a")
	now := time.Now()
	past := now.Add(-time.Hour)

	live := mustEntry(t, r, ctx, space.ID, "still true")
	expired := mustEntry(t, r, ctx, space.ID, "no longer true")
	if err := r.ExpireEntry(ctx, expired.ID, past); err != nil {
		t.Fatalf("expire: %v", err)
	}
	old := mustEntry(t, r, ctx, space.ID, "replaced")
	replacement := mustEntry(t, r, ctx, space.ID, "replacement")
	if err := r.SupersedeEntry(ctx, old.ID, replacement.ID); err != nil {
		t.Fatalf("supersede: %v", err)
	}

	got, err := r.ListValid(ctx, space.ID, now)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	ids := map[string]bool{}
	for _, e := range got {
		ids[e.ID] = true
	}
	if !ids[live.ID] || !ids[replacement.ID] {
		t.Error("a live entry and a replacement must both be returned")
	}
	if ids[expired.ID] {
		t.Error("an expired entry must not be returned")
	}
	if ids[old.ID] {
		t.Error("a superseded entry must not be returned")
	}
}

func TestSupersedeDoesNotDelete(t *testing.T) {
	r, ctx := newMemoryRepo(t)
	space := mustSpace(t, r, ctx, "project-a")
	old := mustEntry(t, r, ctx, space.ID, "replaced")
	replacement := mustEntry(t, r, ctx, space.ID, "replacement")
	if err := r.SupersedeEntry(ctx, old.ID, replacement.ID); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	got, err := r.GetEntry(ctx, old.ID)
	if err != nil {
		t.Fatalf("the superseded row must still exist: %v", err)
	}
	if got.SupersededBy == nil || *got.SupersededBy != replacement.ID {
		t.Error("superseded_by must point at the replacement — the chain is the audit trail")
	}
}

func TestCreateSpaceIsIdempotent(t *testing.T) {
	r, ctx := newMemoryRepo(t)
	first := mustSpace(t, r, ctx, "project-a")
	second, err := r.CreateSpace(ctx, repo.CreateSpaceInput{
		Slug:  "project-a",
		Name:  "renamed",
		Scope: repo.GlobalScope(),
	})
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("creating a space with the same slug/scope created a second row: %s != %s", first.ID, second.ID)
	}
	if second.Name != "renamed" {
		t.Errorf("name = %q, want the refreshed value", second.Name)
	}
}

func TestGetSpaceAndListSpaces(t *testing.T) {
	r, ctx := newMemoryRepo(t)
	space := mustSpace(t, r, ctx, "project-a")

	got, err := r.GetSpace(ctx, repo.GlobalScope(), "project-a")
	if err != nil {
		t.Fatalf("get space: %v", err)
	}
	if got.ID != space.ID {
		t.Errorf("GetSpace returned a different row: %s != %s", got.ID, space.ID)
	}

	list, err := r.ListSpaces(ctx, repo.GlobalScope())
	if err != nil {
		t.Fatalf("list spaces: %v", err)
	}
	if len(list) != 1 || list[0].ID != space.ID {
		t.Errorf("ListSpaces = %v, want exactly the one created space", list)
	}
}

func TestDeleteSpaceRefusesWhenEntriesReferenceIt(t *testing.T) {
	r, ctx := newMemoryRepo(t)
	space := mustSpace(t, r, ctx, "project-a")
	entry := mustEntry(t, r, ctx, space.ID, "still referenced")
	// Even an expired entry must still block the delete: it references the
	// space just as much as a live one, and deleting past that would leave
	// exactly the dangling reference this guard exists to prevent.
	if err := r.ExpireEntry(ctx, entry.ID, time.Now()); err != nil {
		t.Fatalf("expire: %v", err)
	}

	err := r.DeleteSpace(ctx, space.ID)
	if !errors.Is(err, repo.ErrResourceReferenced) {
		t.Fatalf("DeleteSpace error = %v, want ErrResourceReferenced", err)
	}

	if _, err := r.GetSpace(ctx, repo.GlobalScope(), "project-a"); err != nil {
		t.Errorf("space must still exist after a refused delete: %v", err)
	}
	if _, err := r.GetEntry(ctx, entry.ID); err != nil {
		t.Errorf("the referencing entry must still exist: %v", err)
	}
}

func TestDeleteSpaceAllowsUnreferenced(t *testing.T) {
	r, ctx := newMemoryRepo(t)
	space := mustSpace(t, r, ctx, "project-a")

	if err := r.DeleteSpace(ctx, space.ID); err != nil {
		t.Fatalf("delete space: %v", err)
	}
	if _, err := r.GetSpace(ctx, repo.GlobalScope(), "project-a"); !repo.IsNotFound(err) {
		t.Errorf("GetSpace after delete = %v, want not-found", err)
	}
}

func TestCreateEntryRejectsUnknownSpace(t *testing.T) {
	r, ctx := newMemoryRepo(t)
	_, err := r.CreateEntry(ctx, repo.CreateEntryInput{
		SpaceID:    "does-not-exist",
		Summary:    "orphan",
		Content:    "orphan",
		Kind:       "fact",
		SourceKind: "user",
		Confidence: 1,
	})
	if err == nil {
		t.Error("an entry referencing an unrecognised space_id must be refused, not written")
	}
}

func TestSupersedeRejectsUnknownReplacement(t *testing.T) {
	r, ctx := newMemoryRepo(t)
	space := mustSpace(t, r, ctx, "project-a")
	old := mustEntry(t, r, ctx, space.ID, "replaced")

	if err := r.SupersedeEntry(ctx, old.ID, "does-not-exist"); err == nil {
		t.Error("superseding with a replacement that does not exist must be refused")
	}

	got, err := r.GetEntry(ctx, old.ID)
	if err != nil {
		t.Fatalf("get entry: %v", err)
	}
	if got.SupersededBy != nil {
		t.Error("a rejected supersede must not have written a dangling pointer")
	}
}

func TestRecordInjectionPersistsEntryIDs(t *testing.T) {
	r, ctx := newMemoryRepo(t)
	space := mustSpace(t, r, ctx, "project-a")
	entry := mustEntry(t, r, ctx, space.ID, "used in a spawn")

	got, err := r.RecordInjection(ctx, repo.RecordInjectionInput{
		StageRunID:     "stage-run-1",
		EntryIDs:       []string{entry.ID},
		CharBudget:     4000,
		CharsUsed:      120,
		CandidateCount: 3,
	})
	if err != nil {
		t.Fatalf("record injection: %v", err)
	}
	if len(got.EntryIds) != 1 || got.EntryIds[0] != entry.ID {
		t.Errorf("entry_ids = %v, want [%s]", got.EntryIds, entry.ID)
	}
	if got.CharBudget != 4000 || got.CharsUsed != 120 || got.CandidateCount != 3 {
		t.Errorf("counters not persisted as given: %+v", got)
	}
}
