package kontorsession

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"

	"github.com/lx-wnk/kontor/server/internal/channelconfig"
	"github.com/lx-wnk/kontor/server/internal/claudeconfig"
	"github.com/lx-wnk/kontor/server/internal/db/repo"
	"github.com/lx-wnk/kontor/server/internal/mcp"
)

//go:embed briefing.md
var briefing string

// SpawnOptions mirrors agents.SessionSpawnOptions field for field, so DI adapts
// with a plain conversion and this package imports nothing from internal/api.
type SpawnOptions struct {
	Cwd                string
	Prompt             string
	AppendSystemPrompt string
	Name               string
	MCPConfigPath      string
	AllowedTools       []string
	OnExit             func(pid int)
}

// Service owns the one Kontor session. The active kontor_session key is the
// session record; mu serialises every transition.
type Service struct {
	Keys       mcp.KontorSessionKeyIssuer
	Audit      repo.AuditEventRepo
	TaskAPIURL string
	Dir        string
	Spawn      func(context.Context, SpawnOptions) (int, error)
	Terminate  func(pid int) error
	Alive      func(pid int) bool
	WaitExit   func(pid int)

	mu      sync.Mutex
	cfgPath string // lost on restart; SweepOrphanedConfigs removes it then
}

func (s *Service) Current(ctx context.Context) (int, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current(ctx)
}

func (s *Service) current(ctx context.Context) (int, bool, error) {
	pid, ok, err := s.Keys.Current(ctx)
	if err != nil || !ok || pid == 0 {
		return 0, false, err
	}
	return pid, true, nil
}

func (s *Service) Start(ctx context.Context, prompt string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pid, ok, err := s.current(ctx); err != nil || ok {
		return pid, err
	}
	return s.start(ctx, prompt)
}

func (s *Service) Renew(ctx context.Context, prompt string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.end(ctx, "renewed"); err != nil {
		return 0, err
	}
	return s.start(ctx, prompt)
}

func (s *Service) End(ctx context.Context, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.end(ctx, reason)
}

// Reconcile runs once at boot: a recorded pid that is gone (or never
// attached) ends the session, a live one gets its exit watcher back.
func (s *Service) Reconcile(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pid, ok, err := s.Keys.Current(ctx)
	if err != nil || !ok {
		return err
	}
	if pid == 0 || !s.Alive(pid) {
		return s.end(ctx, "process gone at boot")
	}
	go func() {
		s.WaitExit(pid)
		s.exited(pid)
	}()
	return nil
}

func (s *Service) start(ctx context.Context, prompt string) (int, error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return 0, fmt.Errorf("kontorsession: session dir: %w", err)
	}
	token, err := s.Keys.Issue(ctx)
	if err != nil {
		return 0, err
	}
	cfg, err := s.writeConfig(token)
	if err != nil {
		return 0, errors.Join(err, s.Keys.Revoke(ctx))
	}
	pid, err := s.Spawn(ctx, SpawnOptions{
		Cwd: s.Dir, Prompt: prompt, AppendSystemPrompt: briefing, Name: "Kontor",
		MCPConfigPath: cfg, AllowedTools: mcp.KontorSessionAllowedTools(), OnExit: s.exited,
	})
	if err == nil {
		err = s.Keys.Attach(ctx, pid)
		if err != nil {
			err = errors.Join(err, s.Terminate(pid))
		}
	}
	if err != nil {
		_ = os.Remove(cfg)
		return 0, errors.Join(err, s.Keys.Revoke(ctx))
	}
	s.cfgPath = cfg
	s.audit(ctx, "kontor_session.start", pid, "")
	return pid, nil
}

// end is the single exit path: stop the process, revoke the key, remove the
// config, audit. Ending no session is not an error.
func (s *Service) end(ctx context.Context, reason string) error {
	pid, ok, err := s.Keys.Current(ctx)
	if err != nil {
		return err
	}
	if ok && pid > 0 && s.Alive(pid) {
		if err := s.Terminate(pid); err != nil {
			return fmt.Errorf("kontorsession: stop pid %d: %w", pid, err)
		}
	}
	if ok {
		if err := s.Keys.Revoke(ctx); err != nil {
			return err
		}
		s.audit(ctx, "kontor_session.end", pid, reason)
	}
	if s.cfgPath != "" {
		_ = os.Remove(s.cfgPath)
		s.cfgPath = ""
	}
	return nil
}

// exited fires from the watcher of every session ever started, so it ends
// only the session still recorded for that pid — never a renewed one.
func (s *Service) exited(pid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	cur, ok, err := s.current(ctx)
	if err != nil || !ok || cur != pid {
		return
	}
	if err := s.end(ctx, "process exited"); err != nil {
		slog.Warn("kontorsession: end after exit failed", "pid", pid, "err", err)
	}
}

func (s *Service) writeConfig(token string) (string, error) {
	self, err := channelconfig.SelfBinaryPath()
	if err != nil {
		return "", fmt.Errorf("kontorsession: self binary: %w", err)
	}
	servers, err := claudeconfig.UserMCPServers()
	if err != nil {
		slog.Warn("kontorsession: user MCP servers unreadable, starting without them", "err", err)
	}
	return channelconfig.WriteTempConfig(self, &channelconfig.TaskAPI{URL: s.TaskAPIURL, Token: token}, servers)
}

func (s *Service) audit(ctx context.Context, action string, pid int, reason string) {
	if s.Audit == nil {
		return
	}
	if err := s.Audit.RecordAudit(ctx, nil, action, strconv.Itoa(pid), map[string]any{"reason": reason}); err != nil {
		slog.Warn("kontorsession: audit write failed", "action", action, "err", err)
	}
}
