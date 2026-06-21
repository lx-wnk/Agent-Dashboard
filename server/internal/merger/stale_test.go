package merger

import (
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

func newTestTracker(channel map[int]bool, sessions map[string]*parser.SessionData) *staleTracker {
	t := newStaleTracker()
	t.channelFn = func(pid int) bool { return channel[pid] }
	t.parseFn = func(path string) (*parser.SessionData, error) { return sessions[path], nil }
	return t
}

func TestStaleTracker_EmitsFinishedForDeadChannelAgent(t *testing.T) {
	tr := newTestTracker(
		map[int]bool{42: true},
		map[string]*parser.SessionData{
			"/p/s1.jsonl": {SessionID: "s1", LastActivity: time.Now(), Model: "claude-opus-4-8"},
		},
	)
	tr.record(42, liveSnapshot{sessionID: "s1", path: "/p/s1.jsonl", projectPath: "/proj", provider: sdk.ProviderClaude})

	stale := tr.buildStale(map[int]bool{}, 0) // pid 42 no longer live

	if len(stale) != 1 {
		t.Fatalf("want 1 stale agent, got %d", len(stale))
	}
	if stale[0].Status != sdk.AgentStatusFinished {
		t.Errorf("status = %q, want finished", stale[0].Status)
	}
	if stale[0].SessionID != "s1" || stale[0].PID != 42 {
		t.Errorf("got sessionID=%q pid=%d", stale[0].SessionID, stale[0].PID)
	}
	if !stale[0].ChannelAvailable || stale[0].LiveInjectable {
		t.Errorf("want ChannelAvailable=true LiveInjectable=false, got %v/%v", stale[0].ChannelAvailable, stale[0].LiveInjectable)
	}
}

func TestStaleTracker_SkipsAndForgetsWhenDiscoveryGone(t *testing.T) {
	channel := map[int]bool{42: true}
	tr := newTestTracker(channel, map[string]*parser.SessionData{"/p/s1.jsonl": {SessionID: "s1", LastActivity: time.Now()}})
	tr.record(42, liveSnapshot{sessionID: "s1", path: "/p/s1.jsonl"})

	channel[42] = false // discovery file deleted (dismissed)
	stale := tr.buildStale(map[int]bool{}, 0)

	if len(stale) != 0 {
		t.Fatalf("want 0 stale agents, got %d", len(stale))
	}
	// second pass must not resurrect it even if the file reappears for a reused pid
	channel[42] = true
	if got := tr.buildStale(map[int]bool{}, 0); len(got) != 0 {
		t.Fatalf("want tracker to have forgotten pid 42, got %d", len(got))
	}
}

func TestStaleTracker_SkipsLivePID(t *testing.T) {
	tr := newTestTracker(map[int]bool{42: true}, map[string]*parser.SessionData{"/p/s1.jsonl": {SessionID: "s1", LastActivity: time.Now()}})
	tr.record(42, liveSnapshot{sessionID: "s1", path: "/p/s1.jsonl"})

	stale := tr.buildStale(map[int]bool{42: true}, 0) // pid 42 still live
	if len(stale) != 0 {
		t.Fatalf("want 0 (pid still live), got %d", len(stale))
	}
}
