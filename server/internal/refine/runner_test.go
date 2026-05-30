package refine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func TestRunner_State_DefaultsToIdle(t *testing.T) {
	r := NewRunner(nil, nil)
	status, errMsg := r.State("task-x")
	if status != StatusIdle {
		t.Errorf("default status: got %q, want %q", status, StatusIdle)
	}
	if errMsg != "" {
		t.Errorf("default errMsg: got %q, want empty", errMsg)
	}
}

func TestRunner_IsRunning_FalseWhenAbsent(t *testing.T) {
	r := NewRunner(nil, nil)
	if r.IsRunning("task-x") {
		t.Error("IsRunning should be false for an unknown task")
	}
	_ = context.Background()
}

// fakeTurns is a minimal in-memory RefinementTurnRepo for runner tests.
type fakeTurns struct {
	mu      sync.Mutex
	created []repo.CreateTurnInput
}

func (f *fakeTurns) Create(_ context.Context, in repo.CreateTurnInput) (*ent.RefinementTurn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, in)
	return &ent.RefinementTurn{}, nil
}

func (f *fakeTurns) ListForTask(context.Context, string, int) ([]*ent.RefinementTurn, error) {
	return nil, nil
}

func (f *fakeTurns) ListForTaskNewest(context.Context, string, int) ([]*ent.RefinementTurn, error) {
	return nil, nil
}

func (f *fakeTurns) DeleteForTask(context.Context, string) error {
	return nil
}

func (f *fakeTurns) assistantCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.created {
		if c.Role == "assistant" {
			n++
		}
	}
	return n
}

// waitFor polls until cond() or the deadline; fails the test on timeout.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", msg)
}

func TestRunner_Start_PersistsAssistantTurnAndMarksDone(t *testing.T) {
	turns := &fakeTurns{}
	spawn := func(_ context.Context, _ SpawnConfig, _ *ent.Spawner) (<-chan string, error) {
		ch := make(chan string, 2)
		ch <- "Hello"
		ch <- "World"
		close(ch)
		return ch, nil
	}
	r := NewRunner(turns, spawn)

	out, err := r.Start("task-1", SpawnConfig{}, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Drain the tee channel like the HTTP handler would.
	for range out { //nolint:revive
	}
	waitFor(t, func() bool {
		s, _ := r.State("task-1")
		return s == StatusDone
	}, "status done")

	if turns.assistantCount() != 1 {
		t.Errorf("assistant turns persisted: got %d, want 1", turns.assistantCount())
	}
}
