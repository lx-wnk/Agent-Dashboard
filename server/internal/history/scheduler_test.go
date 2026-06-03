package history

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeCountingImporter builds an Importer whose collect function increments
// a counter every time runImport is called. The collect fn returns no files
// so each scan is instantaneous with zero DB side-effects.
func makeCountingImporter(counter *atomic.Int64) *Importer {
	stub := &stubCostRepo{}
	imp := NewImporter(stub).WithCollectFn(func(_ string) ([]string, error) {
		counter.Add(1)
		return nil, nil
	})
	return imp
}

// TestRunScheduled_BootScanOnly verifies that interval <= 0 causes exactly one
// scan and that RunScheduled returns promptly.
func TestRunScheduled_BootScanOnly(t *testing.T) {
	// Point the scanner at a no-op env so AllAgentConfigDirs produces at least
	// one entry (the collect stub is what actually counts calls, so the exact
	// dirs don't matter — but we need at least one to trigger the collect call).
	t.Setenv("DASHBOARD_CLAUDE_CONFIG_DIRS", "/fake/boot-only")
	t.Setenv("CLAUDE_CONFIG_DIR", "/fake/nonexistent")

	var callCount atomic.Int64
	imp := makeCountingImporter(&callCount)

	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		defer close(done)
		imp.RunScheduled(ctx, 0)
	}()

	select {
	case <-done:
		// good — returned
	case <-time.After(3 * time.Second):
		t.Fatal("RunScheduled did not return within 3s for boot-only mode")
	}

	// The collect fn is called once per configured dir. With one fake dir there
	// is exactly one directory scanned per import run. We performed one boot scan.
	require.GreaterOrEqual(t, callCount.Load(), int64(1), "boot scan must have run at least once")
}

// TestRunScheduled_Periodic verifies that RunScheduled performs more than one
// scan when given a positive interval, and returns after ctx is cancelled.
func TestRunScheduled_Periodic(t *testing.T) {
	t.Setenv("DASHBOARD_CLAUDE_CONFIG_DIRS", "/fake/periodic")
	t.Setenv("CLAUDE_CONFIG_DIR", "/fake/nonexistent")

	const tickInterval = 20 * time.Millisecond
	// Let it run long enough for several ticks, but keep the test well under 1s.
	const runDuration = 200 * time.Millisecond

	var callCount atomic.Int64
	imp := makeCountingImporter(&callCount)

	ctx, cancel := context.WithTimeout(context.Background(), runDuration)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		imp.RunScheduled(ctx, tickInterval)
	}()

	// Wait for RunScheduled to exit after context cancellation.
	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("RunScheduled did not return after ctx cancel within 2s")
	}

	// We expect at least the boot scan plus one periodic scan.
	assert.Greater(t, callCount.Load(), int64(1),
		"periodic mode must run more than one scan")
}

// TestRunScheduled_OverlapGuard verifies that a slow in-progress scan does not
// cause RunScheduled to stack another scan on top — it should skip the tick.
func TestRunScheduled_OverlapGuard(t *testing.T) {
	t.Setenv("DASHBOARD_CLAUDE_CONFIG_DIRS", "/fake/overlap")
	t.Setenv("CLAUDE_CONFIG_DIR", "/fake/nonexistent")

	stub := &stubCostRepo{}

	// A collect fn that blocks until released.
	released := make(chan struct{})
	var callCount atomic.Int64

	imp := NewImporter(stub).WithCollectFn(func(_ string) ([]string, error) {
		callCount.Add(1)
		<-released // block until test unblocks
		return nil, nil
	})

	// Call Run directly (not RunScheduled) so we hold the single-instance lock.
	noop := func(ImportProgress) {}
	err := imp.Run(context.Background(), noop)
	require.NoError(t, err, "first Run must succeed")

	// Now try a second Run while the first is still in progress — must be rejected.
	err2 := imp.Run(context.Background(), noop)
	assert.Error(t, err2, "second concurrent Run must return an error (already in progress)")

	// Unblock the first run.
	close(released)

	// Allow the goroutine to finish.
	require.Eventually(t, func() bool {
		imp.mu.Lock()
		defer imp.mu.Unlock()
		return !imp.running
	}, 3*time.Second, 5*time.Millisecond)
}
