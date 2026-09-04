package pipeline

import "sync"

// spawnCleanupRegistry holds the cleanup closure SpawnStageAgent returns for a
// stage run, so the artifacts a spawn leaves on disk are removed at the same
// moment the run's MCP credentials are revoked: the temp --mcp-config file
// carrying the run's bearer token, and the settings allow-list entries.
//
// Registration is in-process only. A restart drops every pending entry and
// leaves those files behind; expires_at remains the only cap on what a token
// in an orphaned file is still worth.
type spawnCleanupRegistry struct {
	mu       sync.Mutex
	cleanups map[string]func()
}

func newSpawnCleanupRegistry() *spawnCleanupRegistry {
	return &spawnCleanupRegistry{cleanups: make(map[string]func())}
}

// register stores cleanup under stageRunID and runs whatever the same run had
// registered before: a respawn (requeue, retry, recovery) writes fresh
// artifacts, and the previous ones are dead the moment the new process starts.
func (r *spawnCleanupRegistry) register(stageRunID string, cleanup func()) {
	if stageRunID == "" || cleanup == nil {
		return
	}
	r.mu.Lock()
	previous := r.cleanups[stageRunID]
	r.cleanups[stageRunID] = cleanup
	r.mu.Unlock()
	if previous != nil {
		previous()
	}
}

// release runs the cleanup registered for stageRunID and drops it. An unknown
// id is a no-op: a synthetic spawn, an HTTP-adapter stage and a run recovered
// after a restart all reach revocation with nothing registered.
func (r *spawnCleanupRegistry) release(stageRunID string) {
	r.mu.Lock()
	cleanup := r.cleanups[stageRunID]
	delete(r.cleanups, stageRunID)
	r.mu.Unlock()
	if cleanup != nil {
		cleanup()
	}
}
