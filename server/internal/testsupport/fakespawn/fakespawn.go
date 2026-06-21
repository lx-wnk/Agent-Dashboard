// Package fakespawn simulates the on-disk + process-scan artifacts a
// controllable Claude agent leaves behind, so flow tests can exercise the
// live → finished → dismiss lifecycle without launching a real process.
package fakespawn

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
	"github.com/lx-wnk/agent-dashboard/server/internal/scanner"
)

// Spawner owns one fake agent's temp HOME and the set of currently-live fake
// processes. Construct it with New; one instance per test.
type Spawner struct {
	Home string

	mu      sync.Mutex
	live    map[int]scanner.ProcessInfo
	nextPID int
}

type SpawnOpts struct {
	CWD            string       // default: <Home>/work/project
	Provider       sdk.Provider // default: sdk.ProviderClaude
	Model          string       // default: "claude-opus-4-8"
	LiveInjectable bool         // when true, discovery file has a non-empty tmuxPane
	NoChannel      bool         // when true, do NOT write a discovery file (plain terminal agent)
}

type Agent struct {
	PID       int
	SessionID string
	CWD       string
	JSONLPath string
}

// New seeds a Spawner rooted at a temp HOME. The starting PID is in the high
// range so it is virtually never a real live process — important so the real
// dismiss handler's liveness guard treats it as dead.
func New(t *testing.T) *Spawner {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	return &Spawner{
		Home:    home,
		live:    map[int]scanner.ProcessInfo{},
		nextPID: 2_000_000_000,
	}
}

func (s *Spawner) Spawn(opts SpawnOpts) Agent {
	cwd := opts.CWD
	if cwd == "" {
		cwd = filepath.Join(s.Home, "work", "project")
	}
	provider := opts.Provider
	if provider == "" {
		provider = sdk.ProviderClaude
	}
	model := opts.Model
	if model == "" {
		model = "claude-opus-4-8"
	}

	s.mu.Lock()
	pid := s.nextPID
	s.nextPID++
	s.mu.Unlock()

	// Valid session UUID derived from the pid so the parser's uuidRE accepts it
	// (8-4-4-4-12 lowercase hex); the pid fills the final 12-hex segment.
	sessionID := fmt.Sprintf("00000000-0000-0000-0000-%012x", pid)

	projectDir := filepath.Join(s.Home, ".claude", "projects", parser.EncodePath(cwd))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		panic(err)
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	lines := `{"type":"user","sessionId":"` + sessionID + `","timestamp":"` + ts +
		`","message":{"role":"user","content":"hi"}}` + "\n" +
		`{"type":"assistant","sessionId":"` + sessionID + `","timestamp":"` + ts +
		`","message":{"role":"assistant","model":"` + model + `","usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"
	jsonlPath := filepath.Join(projectDir, sessionID+".jsonl")
	if err := os.WriteFile(jsonlPath, []byte(lines), 0o644); err != nil {
		panic(err)
	}

	if !opts.NoChannel {
		discoveryDir := filepath.Join(s.Home, channelconfig.DiscoveryDir)
		if err := os.MkdirAll(discoveryDir, 0o755); err != nil {
			panic(err)
		}
		content := `{"port":1}`
		if opts.LiveInjectable {
			content = `{"port":1,"tmuxPane":"%1"}`
		}
		if err := os.WriteFile(filepath.Join(discoveryDir, fmt.Sprintf("%d.json", pid)), []byte(content), 0o644); err != nil {
			panic(err)
		}
	}

	s.mu.Lock()
	s.live[pid] = scanner.ProcessInfo{PID: pid, CWD: cwd, Uptime: 30, Provider: provider}
	s.mu.Unlock()

	return Agent{PID: pid, SessionID: sessionID, CWD: cwd, JSONLPath: jsonlPath}
}

// Exit removes the process from the scan set; on-disk files remain so the agent
// becomes a finished card.
func (s *Spawner) Exit(pid int) {
	s.mu.Lock()
	delete(s.live, pid)
	s.mu.Unlock()
}

// Dismiss removes the discovery files, mirroring the real DELETE endpoint for
// tests that don't drive the HTTP handler.
func (s *Spawner) Dismiss(pid int) error {
	dir := filepath.Join(s.Home, channelconfig.DiscoveryDir)
	for _, name := range []string{fmt.Sprintf("%d.json", pid), fmt.Sprintf("%d.pty.json", pid)} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// ScanFn returns a scan closure suitable for merger.SetScanProcessesForTest.
func (s *Spawner) ScanFn() func(context.Context) ([]scanner.ProcessInfo, error) {
	return func(context.Context) ([]scanner.ProcessInfo, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		out := make([]scanner.ProcessInfo, 0, len(s.live))
		for _, p := range s.live {
			out = append(out, p)
		}
		return out, nil
	}
}

func (s *Spawner) DiscoveryPath(pid int) string {
	return filepath.Join(s.Home, channelconfig.DiscoveryDir, fmt.Sprintf("%d.json", pid))
}
