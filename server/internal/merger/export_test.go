package merger

import (
	"context"

	"github.com/lx-wnk/agent-dashboard/server/internal/scanner"
)

// SetScanProcessesForTest overrides the process scanner used by GetAgents and
// returns a restore function. Test-only seam: lets tests feed a synthetic
// process list instead of spawning real CLIs.
func SetScanProcessesForTest(fn func(ctx context.Context) ([]scanner.ProcessInfo, error)) func() {
	prev := scanProcessesFn
	scanProcessesFn = fn
	return func() { scanProcessesFn = prev }
}

// ResetStaleTrackerForTest re-initialises the package stale tracker so a test
// starts with no recorded snapshots. Test-only seam.
func ResetStaleTrackerForTest() { resetStaleTracker() }
