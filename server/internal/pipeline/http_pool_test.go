package pipeline

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// liveNumberRepo is a PipelineConfigRepo whose numeric value can be changed
// from another goroutine while the pool under test is running, so a test can
// raise maxParallelOrchestrators the way the settings UI does.
type liveNumberRepo struct {
	fakeConfigRepo
	value atomic.Int64
}

func (r *liveNumberRepo) GetNumber(context.Context, string, float64) float64 {
	return float64(r.value.Load())
}

func newTestPool(t *testing.T, limit int64) (*httpPool, *liveNumberRepo) {
	t.Helper()
	cfg := &liveNumberRepo{}
	cfg.value.Store(limit)
	p := newHTTPPool(newConfigCache(cfg))
	p.poll = 5 * time.Millisecond
	return p, cfg
}

func (p *httpPool) inFlightForTest() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

func TestHTTPPool_AcquiresUpToLiveLimit(t *testing.T) {
	p, _ := newTestPool(t, defaultMaxParallel+3)

	done := make(chan struct{})
	go func() {
		for i := 0; i < defaultMaxParallel+3; i++ {
			p.acquire()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("acquire blocked after %d slots, want %d: pool is not sized from the live maxParallelOrchestrators value",
			p.inFlightForTest(), defaultMaxParallel+3)
	}
}

func TestHTTPPool_BlocksBeyondLiveLimit(t *testing.T) {
	p, _ := newTestPool(t, 2)
	p.acquire()
	p.acquire()

	third := make(chan struct{})
	go func() {
		p.acquire()
		close(third)
	}()

	select {
	case <-third:
		t.Fatal("third acquire granted while maxParallelOrchestrators is 2")
	case <-time.After(50 * time.Millisecond):
	}

	p.release()

	select {
	case <-third:
	case <-time.After(2 * time.Second):
		t.Fatal("third acquire never granted after a slot was released")
	}
}

func TestHTTPPool_RaisedLimitUnblocksParkedWaiter(t *testing.T) {
	p, cfg := newTestPool(t, 2)
	p.acquire()
	p.acquire()

	third := make(chan struct{})
	go func() {
		p.acquire()
		close(third)
	}()

	select {
	case <-third:
		t.Fatal("third acquire granted before maxParallelOrchestrators was raised")
	case <-time.After(50 * time.Millisecond):
	}

	cfg.value.Store(4)
	p.cache.Invalidate()

	select {
	case <-third:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter still parked after maxParallelOrchestrators was raised — the pool went stale")
	}
}
