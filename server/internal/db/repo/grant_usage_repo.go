package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/grantusage"
)

// GrantUsageRepo records uses of a rate-limited grant and answers how many
// fall inside a sliding window.
type GrantUsageRepo interface {
	// RecordIfWithinLimit atomically checks limit/window and records one use
	// of grantID inside a single write transaction (repo.WithTx), so the
	// check and the insert cannot interleave with a concurrent caller's —
	// SQLite serializes writers, so a write transaction is what makes the
	// pair atomic. limit and its WithinLimit semantics (0 unlimited,
	// negative exhausted) are capability.WithinLimit's, reused here rather
	// than reimplemented, so the boundary can never drift between the two.
	//
	// It returns (false, nil) — not an error — when the grant is already
	// exhausted; nothing is recorded in that case. Fail-closed: an error
	// counting or inserting usage also resolves to (false, err) — a usage
	// lookup that cannot be trusted must never be read as permission to
	// proceed.
	RecordIfWithinLimit(ctx context.Context, grantID string, limit int, window time.Duration) (bool, error)
	// CountSince reports how many uses of grantID fall at or after since.
	// Read-only: a caller that needs to act on the result (check-then-use)
	// must call RecordIfWithinLimit instead, which alone closes the race
	// this method cannot.
	CountSince(ctx context.Context, grantID string, since time.Time) (int, error)
}

type entGrantUsageRepo struct {
	client *ent.Client
}

// NewGrantUsageRepo returns a GrantUsageRepo backed by the ent client.
func NewGrantUsageRepo(client *ent.Client) GrantUsageRepo {
	return &entGrantUsageRepo{client: client}
}

func (r *entGrantUsageRepo) RecordIfWithinLimit(ctx context.Context, grantID string, limit int, window time.Duration) (bool, error) {
	if grantID == "" {
		return false, fmt.Errorf("grant_usage.RecordIfWithinLimit: grant_id is required")
	}
	permitted := false
	err := WithTx(ctx, r.client, func(tx *ent.Tx) error {
		used, err := countGrantUsageSince(ctx, tx.GrantUsage, grantID, time.Now().Add(-window))
		if err != nil {
			return err
		}
		if !capability.WithinLimit(capability.GrantView{LimitCount: limit}, used) {
			return nil // exhausted: tx has nothing to commit, permitted stays false
		}
		if err := tx.GrantUsage.Create().
			SetID(uuid.New().String()).
			SetGrantID(grantID).
			Exec(ctx); err != nil {
			return err
		}
		permitted = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("grant_usage.RecordIfWithinLimit: %w", err)
	}
	return permitted, nil
}

func (r *entGrantUsageRepo) CountSince(ctx context.Context, grantID string, since time.Time) (int, error) {
	count, err := countGrantUsageSince(ctx, r.client.GrantUsage, grantID, since)
	if err != nil {
		return 0, fmt.Errorf("grant_usage.CountSince: %w", err)
	}
	return count, nil
}

// countGrantUsageSince is shared by CountSince and RecordIfWithinLimit so
// the two never drift onto a different notion of "in the window" — one
// takes the plain client, the other the client bound to its transaction,
// both the same *ent.GrantUsageClient type.
func countGrantUsageSince(ctx context.Context, gu *ent.GrantUsageClient, grantID string, since time.Time) (int, error) {
	return gu.Query().
		Where(
			grantusage.GrantIDEQ(grantID),
			grantusage.UsedAtGTE(since),
		).
		Count(ctx)
}
