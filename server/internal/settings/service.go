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

// LiveHook is invoked after a successful Set of an ApplyLive key, so a
// subsystem (e.g. the plugin registry) can apply the change without a restart.
type LiveHook func(ctx context.Context, key, value string) error

// Service reads settings DB-first with registry-default fallback.
type Service struct {
	repo Repo

	mu        sync.RWMutex
	snapshot  map[string]string // key -> raw DB value (present only if a row exists)
	liveHooks map[string]LiveHook
}

// New builds a Service.
func New(repo Repo) *Service {
	return &Service{repo: repo, snapshot: map[string]string{}, liveHooks: map[string]LiveHook{}}
}

// RegisterLiveHook attaches a hook for an ApplyLive key.
func (s *Service) RegisterLiveHook(key string, fn LiveHook) {
	s.mu.Lock()
	s.liveHooks[key] = fn
	s.mu.Unlock()
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
func (s *Service) Bool(key string) bool      { b, _ := strconv.ParseBool(s.raw(key)); return b }
func (s *Service) Int(key string) int        { n, _ := strconv.Atoi(s.raw(key)); return n }
func (s *Service) Float(key string) float64  { f, _ := strconv.ParseFloat(s.raw(key), 64); return f }

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

// Set validates against the registry, persists, updates the snapshot, and runs
// the live hook (if any). Returns the definition's Apply semantics.
func (s *Service) Set(ctx context.Context, key, value string) error {
	def, ok := Lookup(key)
	if !ok {
		return fmt.Errorf("settings.Set: unknown key %q", key)
	}
	if err := def.Validate(value); err != nil {
		return fmt.Errorf("settings.Set: %w", err)
	}
	if err := s.repo.Set(ctx, key, value); err != nil {
		return fmt.Errorf("settings.Set: %w", err)
	}
	s.mu.Lock()
	s.snapshot[key] = value
	hook := s.liveHooks[key]
	s.mu.Unlock()
	if def.Apply == ApplyLive && hook != nil {
		if err := hook(ctx, key, value); err != nil {
			return fmt.Errorf("settings.Set live-apply: %w", err)
		}
	}
	return nil
}

// ApplyOf reports the apply-semantics of a key (for API responses).
func (s *Service) ApplyOf(key string) Apply {
	if d, ok := Lookup(key); ok {
		return d.Apply
	}
	return ApplyRestart
}
