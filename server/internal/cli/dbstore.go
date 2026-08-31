package cli

import (
	"context"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/settings"
)

// dbStore opens the dashboard SQLite file directly (no HTTP), so settings can
// be changed while the server is down — the lockout-safe escape hatch.
type dbStore struct {
	client     *ent.Client
	repo       repo.AppSettingRepo
	grants     repo.GrantRepo
	grantUsage repo.GrantUsageRepo
	caps       repo.CapabilityRepo
}

// openDBStore opens (and migrates) the dashboard DB at path.
func openDBStore(path string) (*dbStore, error) {
	bundle, err := db.Open(path)
	if err != nil {
		return nil, err
	}
	return &dbStore{
		client:     bundle.Client,
		repo:       repo.NewAppSettingRepo(bundle.Client),
		grants:     repo.NewGrantRepo(bundle.Client),
		grantUsage: repo.NewGrantUsageRepo(bundle.Client, bundle.WriteClient),
		caps:       repo.NewCapabilityRepo(bundle.Client),
	}, nil
}

func (s *dbStore) Close() error { return s.client.Close() }

func (s *dbStore) Get(ctx context.Context, key string) (string, bool, error) {
	return s.repo.Get(ctx, key)
}

func (s *dbStore) List(ctx context.Context) (map[string]string, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		m[r.Key] = r.Value
	}
	return m, nil
}

func (s *dbStore) Set(ctx context.Context, key, value string) error {
	_, err := s.repo.Upsert(ctx, key, value)
	return err
}

// SetValidated checks the registry before writing.
func (s *dbStore) SetValidated(ctx context.Context, key, value string) error {
	def, ok := settings.Lookup(key)
	if !ok {
		return errUnknownKey(key)
	}
	if err := def.Validate(value); err != nil {
		return err
	}
	return s.Set(ctx, key, value)
}

func errUnknownKey(key string) error { return fmt.Errorf("unknown setting key %q", key) }
