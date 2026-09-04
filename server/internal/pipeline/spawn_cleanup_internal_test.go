package pipeline

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSpawnCleanupRegistry_ReleaseRunsTheCleanupOnce(t *testing.T) {
	r := newSpawnCleanupRegistry()
	calls := 0
	r.register("run-1", func() { calls++ })

	r.release("run-1")
	r.release("run-1")

	require.Equal(t, 1, calls, "release must run the cleanup and then drop it")
}

func TestSpawnCleanupRegistry_ReleaseOfAnUnknownRunIsANoOp(t *testing.T) {
	r := newSpawnCleanupRegistry()
	require.NotPanics(t, func() { r.release("never-registered") },
		"synthetic spawns, HTTP-adapter stages and runs recovered after a restart all release with nothing registered")
}

// TestSpawnCleanupRegistry_ReRegisterRunsThePrevious pins the respawn case: a
// requeued or retried run spawns again and writes a second temp config. Without
// running the superseded cleanup here, the first file — carrying the earlier
// bearer token — stays on disk with nothing left to remove it.
func TestSpawnCleanupRegistry_ReRegisterRunsThePrevious(t *testing.T) {
	r := newSpawnCleanupRegistry()
	var order []string
	r.register("run-1", func() { order = append(order, "first") })
	r.register("run-1", func() { order = append(order, "second") })

	require.Equal(t, []string{"first"}, order, "re-registering must clean up what it replaces")

	r.release("run-1")
	require.Equal(t, []string{"first", "second"}, order, "release must run the surviving cleanup")
}

func TestSpawnCleanupRegistry_IgnoresEmptyIDAndNilCleanup(t *testing.T) {
	r := newSpawnCleanupRegistry()
	r.register("", func() { t.Fatal("a cleanup registered under an empty id is unreachable and must be dropped") })
	r.register("run-1", nil)
	require.NotPanics(t, func() { r.release("run-1") })
}

func TestSpawnCleanupRegistry_ConcurrentRegisterAndRelease(t *testing.T) {
	r := newSpawnCleanupRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); r.register("run-1", func() {}) }()
		go func() { defer wg.Done(); r.release("run-1") }()
	}
	wg.Wait()
}
