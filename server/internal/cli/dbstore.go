package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/secretbox"
	"github.com/lx-wnk/agent-dashboard/server/internal/settings"
)

// ErrNoMasterKey is returned by SetValidated for a secret key when the
// secretbox master key could not be resolved. The CLI is the lockout
// recovery path, so it refuses the write instead of ever storing a secret
// in clear.
var ErrNoMasterKey = errors.New("cli: secretbox master key unavailable")

// dbStore opens the dashboard SQLite file directly (no HTTP), so settings can
// be changed while the server is down — the lockout-safe escape hatch.
type dbStore struct {
	client     *ent.Client
	repo       repo.AppSettingRepo
	grants     repo.GrantRepo
	grantUsage repo.GrantUsageRepo
	caps       repo.CapabilityRepo
	box        *secretbox.Box
	boxErr     error
}

// openDBStore opens (and migrates) the dashboard DB at path. The secretbox
// master key is resolved the same way the server does (serverapp/di.go):
// DASHBOARD_SECRET_KEY if set, else the persisted/generated key file.
// Resolution failure is kept on the store rather than returned here — list,
// get, and non-secret set must keep working even if the master key can't be
// resolved; only SetValidated on a secret key turns it into a hard error.
func openDBStore(path string) (*dbStore, error) {
	bundle, err := db.Open(path)
	if err != nil {
		return nil, err
	}
	box, boxErr := loadSecretBox()
	return &dbStore{
		client:     bundle.Client,
		repo:       repo.NewAppSettingRepo(bundle.Client),
		grants:     repo.NewGrantRepo(bundle.Client),
		grantUsage: repo.NewGrantUsageRepo(bundle.Client, bundle.WriteClient),
		caps:       repo.NewCapabilityRepo(bundle.Client),
		box:        box,
		boxErr:     boxErr,
	}, nil
}

// loadSecretBox resolves the secretbox master key exactly as the server does
// (serverapp/di.go) and builds a Box from it.
func loadSecretBox() (*secretbox.Box, error) {
	masterKey, err := secretbox.LoadOrGenerateMasterKey(os.Getenv("DASHBOARD_SECRET_KEY"))
	if err != nil {
		return nil, fmt.Errorf("resolve master key: %w", err)
	}
	box, err := secretbox.New(masterKey)
	if err != nil {
		return nil, fmt.Errorf("build secret box: %w", err)
	}
	return box, nil
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

// SetValidated checks the registry before writing. A secret definition is
// routed through the secretbox box resolved in openDBStore and never written
// in clear; if the master key could not be resolved, the write is refused
// with ErrNoMasterKey rather than silently falling back to plaintext.
func (s *dbStore) SetValidated(ctx context.Context, key, value string) error {
	def, ok := settings.Lookup(key)
	if !ok {
		return errUnknownKey(key)
	}
	if err := def.Validate(value); err != nil {
		return err
	}
	if !def.Secret {
		return s.Set(ctx, key, value)
	}
	if s.box == nil {
		return fmt.Errorf("%w: cannot set %q: %v", ErrNoMasterKey, key, s.boxErr)
	}
	ciphertext, nonce, err := s.box.Encrypt(value)
	if err != nil {
		return fmt.Errorf("encrypt %q: %w", key, err)
	}
	_, err = s.repo.UpsertSecret(ctx, key, ciphertext, nonce)
	return err
}

func errUnknownKey(key string) error { return fmt.Errorf("unknown setting key %q", key) }
