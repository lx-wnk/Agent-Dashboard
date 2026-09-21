package kontorsession_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/kontor/server/internal/db"
	"github.com/lx-wnk/kontor/server/internal/db/ent"
	"github.com/lx-wnk/kontor/server/internal/db/repo"
	"github.com/lx-wnk/kontor/server/internal/kontorsession"
	"github.com/lx-wnk/kontor/server/internal/mcp"
)

type fakeProc struct {
	mu           sync.Mutex
	next         int
	alive        map[int]bool
	events       []string
	spawns       []kontorsession.SpawnOptions
	failErr      error
	terminateErr error
	exits        map[int]chan struct{}
}

func (f *fakeProc) spawn(_ context.Context, o kontorsession.SpawnOptions) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failErr != nil {
		return 0, f.failErr
	}
	f.next++
	pid := 1000 + f.next
	f.alive[pid] = true
	f.spawns = append(f.spawns, o)
	f.events = append(f.events, "spawn")
	return pid, nil
}
func (f *fakeProc) terminate(pid int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.terminateErr != nil {
		return f.terminateErr
	}
	f.alive[pid] = false
	f.events = append(f.events, "terminate")
	return nil
}
func (f *fakeProc) isAlive(pid int) bool { f.mu.Lock(); defer f.mu.Unlock(); return f.alive[pid] }
func (f *fakeProc) wait(pid int)         { <-f.exits[pid] }

// flakyOnce wraps a real ApiKeyRepo and fails the first ActiveKontorSession
// call after it is installed, then delegates every call after that.
type flakyOnce struct {
	repo.ApiKeyRepo
	failed bool
}

func (f *flakyOnce) ActiveKontorSession(ctx context.Context) (*ent.ApiKey, error) {
	if !f.failed {
		f.failed = true
		return nil, errors.New("database is locked")
	}
	return f.ApiKeyRepo.ActiveKontorSession(ctx)
}

func newService(t *testing.T) (*kontorsession.Service, *fakeProc, repo.ApiKeyRepo) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	keys := repo.NewApiKeyRepo(bundle.Client)
	f := &fakeProc{alive: map[int]bool{}, exits: map[int]chan struct{}{}}
	return &kontorsession.Service{
		Keys: mcp.KontorSessionKeyIssuer{Keys: keys}, TaskAPIURL: "http://127.0.0.1:1/api/mcp",
		Dir: t.TempDir(), ConfigPath: filepath.Join(t.TempDir(), "session-mcp.json"),
		Spawn: f.spawn, Terminate: f.terminate, Alive: f.isAlive, WaitExit: f.wait,
	}, f, keys
}

func TestStart_OneSessionAtATime(t *testing.T) {
	s, f, _ := newService(t)
	var wg sync.WaitGroup
	pids := make([]int, 2)
	for i := range pids {
		wg.Go(func() {
			pid, err := s.Start(t.Context(), "hallo")
			assert.NoError(t, err)
			pids[i] = pid
		})
	}
	wg.Wait()
	require.Equal(t, pids[0], pids[1])
	require.Len(t, f.spawns, 1)

	o := f.spawns[0]
	require.Equal(t, s.Dir, o.Cwd)
	require.Equal(t, "Kontor", o.Name)
	require.NotEmpty(t, o.AppendSystemPrompt)
	require.Equal(t, mcp.KontorSessionAllowedTools(), o.AllowedTools)
	require.Equal(t, s.ConfigPath, o.MCPConfigPath)
	cfg, err := os.ReadFile(o.MCPConfigPath)
	require.NoError(t, err)
	require.Contains(t, string(cfg), mcp.ServerName)
}

func TestStart_FailedSpawnRevokesTheKey(t *testing.T) {
	s, f, keys := newService(t)
	f.failErr = errors.New("boom")
	_, err := s.Start(t.Context(), "hallo")
	require.ErrorContains(t, err, "boom")
	k, err := keys.ActiveKontorSession(t.Context())
	require.NoError(t, err)
	require.Nil(t, k)
}

func TestStart_ClientDisconnectMidSpawnStillRevokesTheKey(t *testing.T) {
	s, _, keys := newService(t)
	ctx, cancel := context.WithCancel(t.Context())
	s.Spawn = func(_ context.Context, _ kontorsession.SpawnOptions) (int, error) {
		cancel() // simulates the request context dying while claude is starting
		return 4242, nil
	}
	_, err := s.Start(ctx, "hallo")
	require.Error(t, err)

	k, err := keys.ActiveKontorSession(context.Background())
	require.NoError(t, err)
	require.Nil(t, k)
}

func TestRenew_EndsBeforeItStarts(t *testing.T) {
	s, f, _ := newService(t)
	first, err := s.Start(t.Context(), "a")
	require.NoError(t, err)
	before, err := os.ReadFile(s.ConfigPath)
	require.NoError(t, err)

	second, err := s.Renew(t.Context(), "b")
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	require.Equal(t, []string{"spawn", "terminate", "spawn"}, f.events)

	after, err := os.ReadFile(s.ConfigPath)
	require.NoError(t, err)
	require.NotEqual(t, before, after) // proves end() removed it and start() rewrote it, not a stale leftover
}

func TestExit_OfAnOldProcessDoesNotEndTheRenewedSession(t *testing.T) {
	s, f, _ := newService(t)
	first, err := s.Start(t.Context(), "a")
	require.NoError(t, err)
	second, err := s.Renew(t.Context(), "b")
	require.NoError(t, err)
	f.spawns[0].OnExit(first)
	pid, ok, err := s.Current(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, second, pid)
}

func TestExit_SurvivesATransientLookupError(t *testing.T) {
	s, f, keys := newService(t)
	pid, err := s.Start(t.Context(), "a")
	require.NoError(t, err)

	s.Keys.Keys = &flakyOnce{ApiKeyRepo: keys}
	f.spawns[0].OnExit(pid)

	k, err := keys.ActiveKontorSession(t.Context())
	require.NoError(t, err)
	require.Nil(t, k)
}

func TestEnd_WithoutSessionIsNotAnError(t *testing.T) {
	s, _, _ := newService(t)
	require.NoError(t, s.End(t.Context(), "operator"))
}

func TestEnd_FailingTerminateKeepsTheKeyActive(t *testing.T) {
	s, f, keys := newService(t)
	_, err := s.Start(t.Context(), "a")
	require.NoError(t, err)

	f.terminateErr = errors.New("kill failed")
	err = s.End(t.Context(), "operator")
	require.Error(t, err)

	k, err := keys.ActiveKontorSession(t.Context())
	require.NoError(t, err)
	require.NotNil(t, k) // operator can retry End once the process is dealt with
}

func TestReconcile_EndsADeadPid(t *testing.T) {
	s, f, keys := newService(t)
	pid, err := s.Start(t.Context(), "a")
	require.NoError(t, err)
	require.FileExists(t, s.ConfigPath)
	f.alive[pid] = false

	restarted := &kontorsession.Service{
		Keys: s.Keys, Dir: s.Dir, ConfigPath: s.ConfigPath,
		Spawn: f.spawn, Terminate: f.terminate, Alive: f.isAlive, WaitExit: f.wait,
	}
	require.NoError(t, restarted.Reconcile(t.Context()))
	k, err := keys.ActiveKontorSession(t.Context())
	require.NoError(t, err)
	require.Nil(t, k)

	_, statErr := os.Stat(s.ConfigPath)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestReconcile_RearmsALivePid(t *testing.T) {
	s, f, keys := newService(t)
	pid, err := s.Start(t.Context(), "a")
	require.NoError(t, err)
	f.exits[pid] = make(chan struct{})
	restarted := &kontorsession.Service{
		Keys: s.Keys, Dir: s.Dir, ConfigPath: s.ConfigPath,
		Spawn: f.spawn, Terminate: f.terminate, Alive: f.isAlive, WaitExit: f.wait,
	}
	require.NoError(t, restarted.Reconcile(t.Context()))
	_, ok, err := restarted.Current(t.Context())
	require.NoError(t, err)
	require.True(t, ok)

	f.mu.Lock()
	f.alive[pid] = false
	f.mu.Unlock()
	close(f.exits[pid])
	require.Eventually(t, func() bool {
		k, _ := keys.ActiveKontorSession(context.Background())
		return k == nil
	}, 2*time.Second, 10*time.Millisecond)
}
