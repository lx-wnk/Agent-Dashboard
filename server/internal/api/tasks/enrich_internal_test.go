package tasks

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/stretchr/testify/require"
)

// TestMemoizeProbe_OnePerDistinctPid verifies the memo calls the underlying probe
// exactly once per distinct pid, regardless of how often a pid recurs.
func TestMemoizeProbe_OnePerDistinctPid(t *testing.T) {
	calls := map[int]int{}
	probe := func(pid int) bool {
		calls[pid]++
		return pid%2 == 0 // arbitrary deterministic result
	}
	isAlive := memoizeProbe(probe)

	for _, pid := range []int{10, 10, 20, 10, 20, 30} {
		isAlive(pid)
	}

	require.Equal(t, 1, calls[10], "pid 10 must be probed once")
	require.Equal(t, 1, calls[20], "pid 20 must be probed once")
	require.Equal(t, 1, calls[30], "pid 30 must be probed once")
	require.True(t, isAlive(10))   // 10 is even → alive, served from cache
	require.False(t, isAlive(15))  // 15 odd → not alive
	require.Equal(t, 1, calls[15]) // and probed once
}

// TestEnrichOne_AwaitingUserZombieGating verifies a dead-pid awaiting_user run with
// pending permissions is flagged blocked/needsUser, while a live pid is not — and
// that isAlive is consulted exactly once for the run's pid.
func TestEnrichOne_AwaitingUserZombieGating(t *testing.T) {
	pid := 4242
	task := &ent.Task{CurrentStage: "implementation"}
	latest := &ent.StageRun{Stage: "implementation", Status: "awaiting_user", Pid: &pid}

	// Dead pid → zombie await; with pending permissions it is blocked + needsUser.
	deadCalls := 0
	deadProbe := memoizeProbe(func(int) bool { deadCalls++; return false })
	dead, err := enrichOne(context.Background(), task, latest, 1, deadProbe, nil, nil, nil)
	require.NoError(t, err)
	require.True(t, dead.BlockedByPendingPermissions)
	require.True(t, dead.NeedsUser)
	require.Equal(t, 1, deadCalls, "isAlive must be consulted once for the run pid")

	// Live pid → not a zombie await; not blocked by the zombie path.
	liveProbe := memoizeProbe(func(int) bool { return true })
	live, err := enrichOne(context.Background(), task, latest, 1, liveProbe, nil, nil, nil)
	require.NoError(t, err)
	require.False(t, live.BlockedByPendingPermissions)
}
