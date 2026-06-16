package system

import (
	"sync"
	"testing"
	"time"
)

// withStubbedCollector swaps the injectable collector and clock for the duration
// of fn, restoring the originals and clearing cached state afterwards. The counter
// it returns reports how many times the collector actually ran.
func withStubbedCollector(now func() time.Time, fn func(count func() int)) {
	origCollect, origNow := siCollect, siNow
	siMu.Lock()
	origCache, origCachedAt, origHas := siCache, siCachedAt, siHasCache
	siCache, siCachedAt, siHasCache = SystemInfo{}, time.Time{}, false
	siMu.Unlock()

	var mu sync.Mutex
	calls := 0
	siCollect = func() SystemInfo {
		mu.Lock()
		calls++
		mu.Unlock()
		return SystemInfo{}
	}
	siNow = now

	defer func() {
		siCollect, siNow = origCollect, origNow
		siMu.Lock()
		siCache, siCachedAt, siHasCache = origCache, origCachedAt, origHas
		siMu.Unlock()
	}()

	fn(func() int {
		mu.Lock()
		defer mu.Unlock()
		return calls
	})
}

// TestCachedSystemInfo_RapidCallsComputeOnce verifies two polls inside the TTL
// window share a single collect, while a poll past the TTL recomputes.
func TestCachedSystemInfo_RapidCallsComputeOnce(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	clock := base
	withStubbedCollector(func() time.Time { return clock }, func(count func() int) {
		cachedSystemInfo()
		clock = base.Add(sysInfoCacheTTL / 2)
		cachedSystemInfo()
		if got := count(); got != 1 {
			t.Fatalf("two calls within TTL should collect once, got %d", got)
		}

		clock = base.Add(sysInfoCacheTTL + time.Millisecond)
		cachedSystemInfo()
		if got := count(); got != 2 {
			t.Fatalf("call past TTL should recompute, got %d collects", got)
		}
	})
}
