package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/grantusage"
)

// GrantUsageRepo records and counts uses of a rate-limited grant. It carries
// no limit logic of its own — that lives in capability.WithinLimit — this
// repo only answers "how many times, since when".
type GrantUsageRepo interface {
	Record(ctx context.Context, grantID string) error
	CountSince(ctx context.Context, grantID string, since time.Time) (int, error)
}

type entGrantUsageRepo struct {
	client *ent.Client
}

// NewGrantUsageRepo returns a GrantUsageRepo backed by the ent client.
func NewGrantUsageRepo(client *ent.Client) GrantUsageRepo {
	return &entGrantUsageRepo{client: client}
}

func (r *entGrantUsageRepo) Record(ctx context.Context, grantID string) error {
	if grantID == "" {
		return fmt.Errorf("grant_usage.Record: grant_id is required")
	}
	if err := r.client.GrantUsage.Create().
		SetID(uuid.New().String()).
		SetGrantID(grantID).
		Exec(ctx); err != nil {
		return fmt.Errorf("grant_usage.Record: %w", err)
	}
	return nil
}

func (r *entGrantUsageRepo) CountSince(ctx context.Context, grantID string, since time.Time) (int, error) {
	count, err := r.client.GrantUsage.Query().
		Where(
			grantusage.GrantIDEQ(grantID),
			grantusage.UsedAtGTE(since),
		).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("grant_usage.CountSince: %w", err)
	}
	return count, nil
}
