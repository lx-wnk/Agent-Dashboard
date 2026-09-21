package kontorsession_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/kontor/server/internal/db"
	"github.com/lx-wnk/kontor/server/internal/db/repo"
	"github.com/lx-wnk/kontor/server/internal/kontorsession"
	"github.com/lx-wnk/kontor/server/internal/mcp"
)

type fakeProc struct {
	mu      sync.Mutex
	next    int
	alive   map[int]bool
	events  []string
	spawns  []kontorsession.SpawnOptions
	failErr error
	exits   map[int]chan struct{}
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
	f.alive[pid] = false
	f.events = append(f.events, "terminate")
	return nil
}
func (f *fakeProc) isAlive(pid int) bool { f.mu.Lock(); defer f.mu.Unlock(); return f.alive[pid] }
func (f *fakeProc) wait(pid int)         { <-f.exits[pid] }

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
		Dir: t.TempDir(), Spawn: f.spawn, Terminate: f.terminate, Alive: f.isAlive, WaitExit: f.wait,
	}, f, keys
}

func TestStart_OneSessionAtATime(t *testing.T) {
	s, f, _ := newService(t)
	var wg sync.WaitGroup
	pids := make([]int, 2)
	for i := range pids {
		wg.Go(func() {
			pid, err := s.Start(t.Context(), "hallo")
			require.NoError(t, err)
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

func TestRenew_EndsBeforeItStarts(t *testing.T) {
	s, f, _ := newService(t)
	first, err := s.Start(t.Context(), "a")
	require.NoError(t, err)
	cfg := f.spawns[0].MCPConfigPath
	second, err := s.Renew(t.Context(), "b")
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	require.Equal(t, []string{"spawn", "terminate", "spawn"}, f.events)
	_, statErr := os.Stat(cfg)
	require.ErrorIs(t, statErr, os.ErrNotExist)
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

func TestEnd_WithoutSessionIsNotAnError(t *testing.T) {
	s, _, _ := newService(t)
	require.NoError(t, s.End(t.Context(), "operator"))
}

func TestReconcile_EndsADeadPid(t *testing.T) {
	s, f, keys := newService(t)
	pid, err := s.Start(t.Context(), "a")
	require.NoError(t, err)
	f.alive[pid] = false
	restarted := &kontorsession.Service{Keys: s.Keys, Dir: s.Dir, Spawn: f.spawn, Terminate: f.terminate, Alive: f.isAlive, WaitExit: f.wait}
	require.NoError(t, restarted.Reconcile(t.Context()))
	k, err := keys.ActiveKontorSession(t.Context())
	require.NoError(t, err)
	require.Nil(t, k)
}

func TestReconcile_RearmsALivePid(t *testing.T) {
	s, f, keys := newService(t)
	pid, err := s.Start(t.Context(), "a")
	require.NoError(t, err)
	f.exits[pid] = make(chan struct{})
	restarted := &kontorsession.Service{Keys: s.Keys, Dir: s.Dir, Spawn: f.spawn, Terminate: f.terminate, Alive: f.isAlive, WaitExit: f.wait}
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
