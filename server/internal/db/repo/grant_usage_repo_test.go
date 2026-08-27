package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func newGrantUsageRepo(t *testing.T) (repo.GrantUsageRepo, *db.DBBundle, context.Context) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	return repo.NewGrantUsageRepo(bundle.Client), bundle, context.Background()
}

func TestGrantUsageRecordRequiresGrantID(t *testing.T) {
	r, _, ctx := newGrantUsageRepo(t)
	if err := r.Record(ctx, ""); err == nil {
		t.Fatal("Record with an empty grant_id must be refused")
	}
}

func TestGrantUsageRecordThenCountSince(t *testing.T) {
	r, _, ctx := newGrantUsageRepo(t)
	grantID := "g1"

	if err := r.Record(ctx, grantID); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := r.Record(ctx, grantID); err != nil {
		t.Fatalf("Record: %v", err)
	}

	count, err := r.CountSince(ctx, grantID, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("CountSince: %v", err)
	}
	if count != 2 {
		t.Errorf("CountSince = %d, want 2", count)
	}
}

// TestGrantUsageCountSinceExcludesRowsOlderThanTheWindow pins the sliding
// window's boundary using rows seeded at explicit, known times rather than
// sleeping — a test that sleeps to prove a window is slow and still proves
// nothing about the boundary.
func TestGrantUsageCountSinceExcludesRowsOlderThanTheWindow(t *testing.T) {
	_, bundle, ctx := newGrantUsageRepo(t)
	grantID := "g1"

	now := time.Now()
	seed := func(usedAt time.Time) {
		t.Helper()
		if err := bundle.Client.GrantUsage.Create().
			SetID(uuid.New().String()).
			SetGrantID(grantID).
			SetUsedAt(usedAt).
			Exec(ctx); err != nil {
			t.Fatalf("seed grant usage: %v", err)
		}
	}

	seed(now.Add(-2 * time.Hour))    // outside a 1h window
	seed(now.Add(-90 * time.Minute)) // outside a 1h window
	seed(now.Add(-30 * time.Minute)) // inside a 1h window
	seed(now.Add(-1 * time.Minute))  // inside a 1h window
	// A different grant's usage must never be counted for this one.
	if err := bundle.Client.GrantUsage.Create().
		SetID(uuid.New().String()).
		SetGrantID("g2").
		SetUsedAt(now).
		Exec(ctx); err != nil {
		t.Fatalf("seed other grant usage: %v", err)
	}

	r := repo.NewGrantUsageRepo(bundle.Client)
	count, err := r.CountSince(ctx, grantID, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("CountSince: %v", err)
	}
	if count != 2 {
		t.Errorf("CountSince = %d, want 2 (rows older than the window and rows for another grant must be excluded)", count)
	}
}

// TestGrantUsageCountSinceBoundaryIsInclusive pins the exact-boundary case:
// a row used exactly at `since` counts as inside the window.
func TestGrantUsageCountSinceBoundaryIsInclusive(t *testing.T) {
	_, bundle, ctx := newGrantUsageRepo(t)
	grantID := "g1"
	since := time.Now().Add(-time.Hour)

	if err := bundle.Client.GrantUsage.Create().
		SetID(uuid.New().String()).
		SetGrantID(grantID).
		SetUsedAt(since).
		Exec(ctx); err != nil {
		t.Fatalf("seed grant usage: %v", err)
	}

	r := repo.NewGrantUsageRepo(bundle.Client)
	count, err := r.CountSince(ctx, grantID, since)
	if err != nil {
		t.Fatalf("CountSince: %v", err)
	}
	if count != 1 {
		t.Errorf("CountSince = %d, want 1 (a row exactly at the boundary is inside the window)", count)
	}
}
