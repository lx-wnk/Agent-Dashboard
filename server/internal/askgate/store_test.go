package askgate

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAskReturnsTheDeliveredDecision(t *testing.T) {
	s := New[string](5*time.Second, nil)
	done := make(chan struct {
		decision string
		ok       bool
	}, 1)
	go func() {
		decision, ok := s.Ask(context.Background(), "id-1", "meta")
		done <- struct {
			decision string
			ok       bool
		}{decision, ok}
	}()

	waitUntil(t, func() bool { return len(s.List()) == 1 })
	if err := s.Resolve("id-1", func(meta string) (string, error) {
		if meta != "meta" {
			t.Fatalf("decide saw meta = %q, want %q", meta, "meta")
		}
		return "allow", nil
	}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	select {
	case got := <-done:
		if !got.ok || got.decision != "allow" {
			t.Fatalf("Ask = (%q, %v), want (\"allow\", true)", got.decision, got.ok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ask never returned")
	}
}

func TestAskTimesOutWithNoDecision(t *testing.T) {
	s := New[string](30*time.Millisecond, nil)
	decision, ok := s.Ask(context.Background(), "id-1", "meta")
	if ok || decision != "" {
		t.Fatalf("Ask = (%q, %v), want (\"\", false) on timeout", decision, ok)
	}
	if got := s.List(); len(got) != 0 {
		t.Fatalf("List after timeout = %+v, want empty — the entry must be cleaned up", got)
	}
}

func TestAskEndsWhenContextIsCancelled(t *testing.T) {
	s := New[string](5*time.Second, nil)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	var decision string
	var ok bool
	go func() {
		decision, ok = s.Ask(ctx, "id-1", "meta")
		close(done)
	}()

	waitUntil(t, func() bool { return len(s.List()) == 1 })
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Ask did not return after ctx was cancelled")
	}
	if ok || decision != "" {
		t.Fatalf("Ask = (%q, %v), want (\"\", false) on ctx cancellation", decision, ok)
	}
}

func TestResolveOnUnknownIDFails(t *testing.T) {
	s := New[string](time.Second, nil)
	err := s.Resolve("nope", func(string) (string, error) { return "allow", nil })
	if !errors.Is(err, ErrNotPending) {
		t.Fatalf("Resolve(unknown) = %v, want ErrNotPending", err)
	}
}

func TestResolveTwiceOnTheSameIDFailsTheSecondTime(t *testing.T) {
	s := New[string](5*time.Second, nil)
	go func() { _, _ = s.Ask(context.Background(), "id-1", "meta") }()
	waitUntil(t, func() bool { return len(s.List()) == 1 })

	if err := s.Resolve("id-1", func(string) (string, error) { return "allow", nil }); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	err := s.Resolve("id-1", func(string) (string, error) { return "deny", nil })
	if !errors.Is(err, ErrNotPending) {
		t.Fatalf("second Resolve = %v, want ErrNotPending", err)
	}
}

func TestResolveVetoLeavesTheRequestPending(t *testing.T) {
	s := New[string](5*time.Second, nil)
	sentinel := errors.New("vetoed")
	done := make(chan struct {
		decision string
		ok       bool
	}, 1)
	go func() {
		decision, ok := s.Ask(context.Background(), "id-1", "meta")
		done <- struct {
			decision string
			ok       bool
		}{decision, ok}
	}()
	waitUntil(t, func() bool { return len(s.List()) == 1 })

	if err := s.Resolve("id-1", func(string) (string, error) { return "", sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("vetoed Resolve = %v, want the sentinel error", err)
	}
	if got := s.List(); len(got) != 1 {
		t.Fatalf("List after a vetoed resolve = %+v, want the request still pending", got)
	}

	if err := s.Resolve("id-1", func(string) (string, error) { return "deny", nil }); err != nil {
		t.Fatalf("Resolve after the veto: %v", err)
	}
	select {
	case got := <-done:
		if !got.ok || got.decision != "deny" {
			t.Fatalf("Ask = (%q, %v), want (\"deny\", true)", got.decision, got.ok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ask never returned")
	}
}

func TestOnChangeFiresOnAddAndOnRemoval(t *testing.T) {
	var fires atomic.Int64
	s := New[string](50*time.Millisecond, func() { fires.Add(1) })

	s.Ask(context.Background(), "id-1", "meta")

	// One fire for the add, one for the removal on timeout.
	if got := fires.Load(); got != 2 {
		t.Fatalf("onChange fired %d times, want 2 (add + remove)", got)
	}
}

func TestConcurrentAskAndResolve(t *testing.T) {
	s := New[int](2*time.Second, nil)
	const n = 50
	var wg sync.WaitGroup
	var delivered, resolved, mismatched atomic.Int64

	for i := range n {
		wg.Add(2)
		id := strconv.Itoa(i)
		go func() {
			defer wg.Done()
			if _, ok := s.Ask(context.Background(), id, i); ok {
				delivered.Add(1)
			}
		}()
		go func() {
			defer wg.Done()
			// Ask may not have registered id yet — retry rather than fail from
			// this goroutine, which the testing package disallows for FailNow.
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				err := s.Resolve(id, func(meta int) (string, error) {
					if meta != i {
						mismatched.Add(1)
					}
					return "allow", nil
				})
				if err == nil {
					resolved.Add(1)
					return
				}
				if !errors.Is(err, ErrNotPending) {
					return
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}
	wg.Wait()

	if got := mismatched.Load(); got != 0 {
		t.Fatalf("decide saw the wrong meta %d time(s) — an id resolved a different request", got)
	}
	if got := resolved.Load(); got != n {
		t.Fatalf("resolved %d of %d requests", got, n)
	}
	if got := delivered.Load(); got != n {
		t.Fatalf("delivered %d of %d concurrent asks", got, n)
	}
	if got := s.List(); len(got) != 0 {
		t.Fatalf("List after all resolved = %+v, want empty", got)
	}
}

func TestAskPanicsOnADuplicateID(t *testing.T) {
	s := New[string](2*time.Second, nil)
	go func() { _, _ = s.Ask(context.Background(), "id-1", "meta-1") }()
	waitUntil(t, func() bool { return len(s.List()) == 1 })

	defer func() {
		if recover() == nil {
			t.Fatal("Ask with an id already pending did not panic")
		}
	}()
	s.Ask(context.Background(), "id-1", "meta-2")
}

// TestResolveWinningTheLapseRaceDeliversItsDecision pins the exact race from
// the review: Ask's timeout branch and a concurrent Resolve both reach for
// the same entry. It uses testBeforeLapseLock to pause Ask right after it has
// committed to the timeout branch but before it takes the lock, so Resolve is
// given the chance to run to completion first — deterministically, not by
// timing luck. Without the fix, Ask would already have decided "", false
// before this checkpoint, disagreeing with Resolve's nil error.
func TestResolveWinningTheLapseRaceDeliversItsDecision(t *testing.T) {
	s := New[string](20*time.Millisecond, nil)
	hookEntered := make(chan struct{})
	proceed := make(chan struct{})
	s.testBeforeLapseLock = func() {
		close(hookEntered)
		<-proceed
	}

	done := make(chan struct {
		decision string
		ok       bool
	}, 1)
	go func() {
		decision, ok := s.Ask(context.Background(), "id-1", "meta")
		done <- struct {
			decision string
			ok       bool
		}{decision, ok}
	}()

	select {
	case <-hookEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("Ask never reached the lapse checkpoint — did the seam get removed?")
	}

	if err := s.Resolve("id-1", func(string) (string, error) { return "allow", nil }); err != nil {
		t.Fatalf("Resolve during the lapse race: %v", err)
	}
	close(proceed)

	select {
	case got := <-done:
		if !got.ok || got.decision != "allow" {
			t.Fatalf("Ask = (%q, %v), want (\"allow\", true) — Resolve won the race and its decision must not be discarded", got.decision, got.ok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ask never returned")
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was never met")
}
