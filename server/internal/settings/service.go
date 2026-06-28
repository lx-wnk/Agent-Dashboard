package settings

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Repo is the persistence the service needs (subset of repo.AppSettingRepo,
// declared locally so tests can fake it).
type Repo interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string) error
	ListAll(ctx context.Context) (map[string]string, error)
}

// ValidationError marks a Set failure caused by an invalid value or unknown key
// (client error), as opposed to a persistence/apply failure (server error).
type ValidationError struct{ Err error }

func (e *ValidationError) Error() string { return e.Err.Error() }
func (e *ValidationError) Unwrap() error { return e.Err }

// Service reads settings DB-first with registry-default fallback.
type Service struct {
	repo Repo

	mu       sync.RWMutex
	snapshot map[string]string // key -> raw DB value (present only if a row exists)
}

// New builds a Service.
func New(repo Repo) *Service {
	return &Service{repo: repo, snapshot: map[string]string{}}
}

// Load reads all rows into the snapshot. Call once at startup.
func (s *Service) Load(ctx context.Context) error {
	all, err := s.repo.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("settings.Load: %w", err)
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

// Set validates against the registry, persists, and updates the snapshot.
// Validation failures are wrapped as *ValidationError (client error);
// persistence failures are returned as plain wrapped errors (server error).
func (s *Service) Set(ctx context.Context, key, value string) error {
	def, ok := Lookup(key)
	if !ok {
		return &ValidationError{Err: fmt.Errorf("settings.Set: unknown key %q", key)}
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
