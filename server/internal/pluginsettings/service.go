// Package pluginsettings manages per-plugin configuration values, encrypting
// secret fields at rest and masking them in API responses.
package pluginsettings

import (
	"context"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
	"github.com/lx-wnk/agent-dashboard/server/internal/secretbox"
)

// MaskedSentinel is returned for secret values and, when sent back on Put,
// signals "leave unchanged".
const MaskedSentinel = "********"

// Stored is one persisted setting row (storage-agnostic).
type Stored struct {
	Key, Value, Nonce string
	Secret            bool
}

// Repo is the persistence the service needs.
type Repo interface {
	ListByPlugin(ctx context.Context, pluginID string) ([]Stored, error)
	Upsert(ctx context.Context, pluginID string, s Stored) error
	DeleteByPlugin(ctx context.Context, pluginID string) error
}

type Service struct {
	repo Repo
	box  *secretbox.Box
}

func New(repo Repo, box *secretbox.Box) *Service { return &Service{repo: repo, box: box} }

func (s *Service) load(ctx context.Context, pluginID string) (map[string]Stored, error) {
	rows, err := s.repo.ListByPlugin(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	m := make(map[string]Stored, len(rows))
	for _, r := range rows {
		m[r.Key] = r
	}
	return m, nil
}

// Get returns key->value for the schema; secret values are masked.
func (s *Service) Get(ctx context.Context, pluginID string, schema []plugin.SettingField) (map[string]string, error) {
	stored, err := s.load(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, f := range schema {
		r, ok := stored[f.Key]
		if !ok {
			out[f.Key] = ""
			continue
		}
		if f.Secret {
			out[f.Key] = MaskedSentinel
		} else {
			out[f.Key] = r.Value
		}
	}
	return out, nil
}

// Put persists values. Secret fields are encrypted; a secret submitted as the
// masked sentinel is skipped (left unchanged). Unknown keys (not in schema) are
// rejected.
func (s *Service) Put(ctx context.Context, pluginID string, schema []plugin.SettingField, values map[string]string) error {
	known := map[string]bool{}
	for _, f := range schema {
		known[f.Key] = true
	}
	for k := range values {
		if !known[k] {
			return fmt.Errorf("pluginsettings: unknown key %q", k)
		}
	}
	for _, f := range schema {
		v, ok := values[f.Key]
		if !ok {
			continue
		}
		if f.Secret {
			if v == MaskedSentinel {
				continue // unchanged
			}
			ct, nonce, err := s.box.Encrypt(v)
			if err != nil {
				return err
			}
			if err := s.repo.Upsert(ctx, pluginID, Stored{Key: f.Key, Value: ct, Nonce: nonce, Secret: true}); err != nil {
				return err
			}
			continue
		}
		if err := s.repo.Upsert(ctx, pluginID, Stored{Key: f.Key, Value: v, Secret: false}); err != nil {
			return err
		}
	}
	return nil
}

// Decrypted returns key->plaintext (secrets decrypted) for env injection (SP2).
func (s *Service) Decrypted(ctx context.Context, pluginID string, schema []plugin.SettingField) (map[string]string, error) {
	stored, err := s.load(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, f := range schema {
		r, ok := stored[f.Key]
		if !ok {
			continue
		}
		if r.Secret {
			pt, derr := s.box.Decrypt(r.Value, r.Nonce)
			if derr != nil {
				return nil, derr
			}
			out[f.Key] = pt
		} else {
			out[f.Key] = r.Value
		}
	}
	return out, nil
}

// Clear removes all settings for a plugin (called on uninstall).
func (s *Service) Clear(ctx context.Context, pluginID string) error {
	return s.repo.DeleteByPlugin(ctx, pluginID)
}
