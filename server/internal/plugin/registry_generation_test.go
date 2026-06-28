package plugin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenerationMismatchBail verifies the generation guard used to prevent a stale
// watcher (from a crashed plugin's backoff window) from clobbering a fresh entry
// created by a concurrent deactivate+reactivate cycle.
//
// A full timing-based integration test for this race would require artificial sleeps
// inside the watcher backoff and is flaky by nature; this focused unit test exercises
// the isStaleGeneration predicate that both check points (post-backoff and lock-held)
// rely on.
func TestGenerationMismatchBail(t *testing.T) {
	r := &Registry{
		generationByID:        make(map[string]int),
		attemptedCapabilities: make(map[string]bool),
	}

	// Simulate first StartOne: entry at generation 1.
	r.mu.Lock()
	r.generationByID["foo"]++
	r.plugins = append(r.plugins, Entry{
		Descriptor: Descriptor{ID: "foo", Addr: "127.0.0.1:1"},
		generation: r.generationByID["foo"],
		healthy:    true,
	})
	r.mu.Unlock()

	// Watcher at gen=1 is not stale while entry is still at gen=1.
	require.False(t, r.isStaleGeneration("foo", 1), "gen-1 watcher must not be stale with gen-1 entry")

	// Simulate deactivate (removeByID) then reactivate (startEntry advances gen).
	r.removeByID("foo")
	r.mu.Lock()
	r.generationByID["foo"]++
	r.plugins = append(r.plugins, Entry{
		Descriptor: Descriptor{ID: "foo", Addr: "127.0.0.1:1"},
		generation: r.generationByID["foo"],
		healthy:    true,
	})
	r.mu.Unlock()

	// Old watcher (gen=1) must now be stale — the registry advanced to gen=2.
	require.True(t, r.isStaleGeneration("foo", 1), "gen-1 watcher must be stale after re-activate at gen=2")
	// New watcher (gen=2) is not stale.
	require.False(t, r.isStaleGeneration("foo", 2), "gen-2 watcher must not be stale")
	// An absent ID is always stale.
	require.True(t, r.isStaleGeneration("bar", 1), "absent ID must always be stale")
}
