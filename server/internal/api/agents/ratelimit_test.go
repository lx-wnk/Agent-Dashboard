package agents

import (
	"testing"
	"time"
)

func TestSlidingWindowLimiter_ClampsNonPositive(t *testing.T) {
	l := newSlidingWindowLimiter(0, 0, 7, 90*time.Second)
	if l.max != 7 {
		t.Fatalf("max = %d, want default 7", l.max)
	}
	if l.window != 90*time.Second {
		t.Fatalf("window = %v, want default 90s", l.window)
	}
}

func TestSlidingWindowLimiter_AllowsUpToMaxThenBlocks(t *testing.T) {
	l := newSlidingWindowLimiter(3, time.Minute, 3, time.Minute)
	const key = "user-1"
	for i := 0; i < 3; i++ {
		if !l.Allow(key) {
			t.Fatalf("attempt %d: Allow=false, want true", i)
		}
		l.Record(key)
	}
	if l.Allow(key) {
		t.Fatal("4th attempt: Allow=true, want false (limit exceeded)")
	}
}

func TestSlidingWindowLimiter_IndependentKeys(t *testing.T) {
	l := newSlidingWindowLimiter(1, time.Minute, 1, time.Minute)
	l.Record("a")
	if l.Allow("a") {
		t.Fatal("key a should be blocked")
	}
	if !l.Allow("b") {
		t.Fatal("key b should be independent and allowed")
	}
}

func TestSlidingWindowLimiter_PruneAllEvictsStale(t *testing.T) {
	l := newSlidingWindowLimiter(5, time.Millisecond, 5, time.Millisecond)
	l.Record("x")
	// Advance past the window, then prune with a future "now".
	l.pruneAll(time.Now().Add(time.Second))
	l.mu.Lock()
	_, present := l.attempts["x"]
	l.mu.Unlock()
	if present {
		t.Fatal("stale key x should have been evicted by pruneAll")
	}
}

func TestSpawnManager_InjectLimiterIndependentOfSpawn(t *testing.T) {
	m := NewSpawnManager(1, 60000, 2, 60000, nil, nil)
	const sub = "u"

	// Exhaust the inject limit (2) without touching the spawn limiter.
	if !m.IsInjectAllowed(sub) {
		t.Fatal("inject 1 should be allowed")
	}
	m.RecordInject(sub)
	if !m.IsInjectAllowed(sub) {
		t.Fatal("inject 2 should be allowed")
	}
	m.RecordInject(sub)
	if m.IsInjectAllowed(sub) {
		t.Fatal("inject 3 should be blocked")
	}
	// Spawn limiter is a separate budget and remains available.
	if !m.IsSpawnAllowed(sub) {
		t.Fatal("spawn limiter must be independent of inject limiter")
	}
}
