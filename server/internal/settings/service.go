package settings

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/lx-wnk/agent-dashboard/server/internal/secretbox"
)

// Repo is the persistence the service needs (subset of repo.AppSettingRepo,
// declared locally so tests can fake it).
type Repo interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string) error
	SetSecret(ctx context.Context, key, ciphertext, nonce string) error
	GetSecret(ctx context.Context, key string) (string, string, bool, error)
	ListAll(ctx context.Context) (map[string]string, error)
}

// ValidationError marks a Set failure caused by an invalid value or unknown key
// (client error), as opposed to a persistence/apply failure (server error).
type ValidationError struct{ Err error }

func (e *ValidationError) Error() string { return e.Err.Error() }
func (e *ValidationError) Unwrap() error { return e.Err }

// ErrNoSecretBox is returned by Secret and by Set for a secret definition
// when the service was built without a *secretbox.Box — e.g. no database is
// configured, so no master key was ever resolved.
var ErrNoSecretBox = errors.New("settings: no secret box configured")

// Service reads settings DB-first with registry-default fallback.
type Service struct {
	repo Repo
	box  *secretbox.Box

	mu       sync.RWMutex
	snapshot map[string]string // key -> raw DB value (present only if a row exists)
}

// New builds a Service. box may be nil when no database is configured; in
// that case secret reads and writes return ErrNoSecretBox instead of
// panicking.
func New(repo Repo, box *secretbox.Box) *Service {
	return &Service{repo: repo, box: box, snapshot: map[string]string{}}
}

// Load reads all rows into the snapshot. Call once at startup.
//
// A row for a secret definition is stored in clear ciphertext by the repo, so
// it is replaced with secretbox.MaskedSentinel here — otherwise Effective()
// would publish base64 ciphertext, which is not a leak of the plaintext but
// is still a value no consumer should see.
func (s *Service) Load(ctx context.Context) error {
	all, err := s.repo.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("settings.Load: %w", err)
	}
	for _, d := range All() {
		if !d.Secret {
			continue
		}
		if _, ok := all[d.Key]; ok {
			all[d.Key] = secretbox.MaskedSentinel
		}
	}
	s.mu.Lock()
	s.snapshot = all
	s.mu.Unlock()
	return nil
}

// raw returns the effective string value: DB row if present, else registry default.
func (s *Service) raw(key string) string {
	s.mu.RLock()
	v, ok := s.snapshot[key]
	s.mu.RUnlock()
	if ok {
		return v
	}
	if d, ok := Lookup(key); ok {
		return d.Default
	}
	return ""
}

// Typed accessors. They assume the key exists in the registry (programmer error otherwise).
func (s *Service) String(key string) string { return s.raw(key) }
func (s *Service) Bool(key string) bool     { b, _ := strconv.ParseBool(s.raw(key)); return b }
func (s *Service) Int(key string) int       { n, _ := strconv.Atoi(s.raw(key)); return n }
func (s *Service) Float(key string) float64 { f, _ := strconv.ParseFloat(s.raw(key), 64); return f }

func (s *Service) StringSlice(key string) []string {
	raw := s.raw(key)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// Effective returns key -> current effective value for every registry key
// (DB value or default). Used by the API to render the settings UI.
func (s *Service) Effective() map[string]string {
	out := map[string]string{}
	for _, d := range All() {
		out[d.Key] = s.raw(d.Key)
	}
	return out
}

// Secret returns the decrypted value of a secret setting. It is the only read
// path that does not mask; every other accessor returns secretbox.MaskedSentinel
// so a secret cannot leak through a surface that was written for plain values.
func (s *Service) Secret(ctx context.Context, key string) (string, error) {
	def, ok := Lookup(key)
	if !ok {
		return "", fmt.Errorf("settings: unknown setting %q", key)
	}
	if !def.Secret {
		return "", fmt.Errorf("settings: %q is not a secret setting", key)
	}
	if s.box == nil {
		return "", ErrNoSecretBox
	}
	ct, nonce, found, err := s.repo.GetSecret(ctx, key)
	if err != nil {
		return "", fmt.Errorf("settings.Secret: %w", err)
	}
	if !found || nonce == "" {
		return "", nil
	}
	return s.box.Decrypt(ct, nonce)
}

// Set validates against the registry, persists, and updates the snapshot.
// Validation failures are wrapped as *ValidationError (client error);
// persistence failures are returned as plain wrapped errors (server error).
func (s *Service) Set(ctx context.Context, key, value string) error {
	def, ok := Lookup(key)
	if !ok {
		return &ValidationError{Err: fmt.Errorf("settings.Set: unknown key %q", key)}
	}
	if def.Secret {
		if value == secretbox.MaskedSentinel {
			return nil // the caller is echoing back what it was shown
		}
		if s.box == nil {
			return ErrNoSecretBox
		}
		ct, nonce, err := s.box.Encrypt(value)
		if err != nil {
			return fmt.Errorf("settings.Set: encrypt %q: %w", key, err)
		}
		if err := s.repo.SetSecret(ctx, key, ct, nonce); err != nil {
			return fmt.Errorf("settings.Set: %w", err)
		}
		s.mu.Lock()
		s.snapshot[key] = secretbox.MaskedSentinel
		s.mu.Unlock()
		return nil
	}
	if err := def.Validate(value); err != nil {
		return &ValidationError{Err: fmt.Errorf("settings.Set: %w", err)}
	}
	if err := s.repo.Set(ctx, key, value); err != nil {
		return fmt.Errorf("settings.Set: %w", err)
	}
	s.mu.Lock()
	s.snapshot[key] = value
	s.mu.Unlock()
	return nil
}

// ApplyOf reports the apply-semantics of a key (for API responses).
func (s *Service) ApplyOf(key string) Apply {
	if d, ok := Lookup(key); ok {
		return d.Apply
	}
	return ApplyRestart
}
