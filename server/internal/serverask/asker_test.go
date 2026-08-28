package serverask

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/askgate"
	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
)

type askResult struct {
	ok  bool
	err error
}

func TestAskFailsClosedOnTimeout(t *testing.T) {
	a := New(nil)
	a.store.SetHoldFor(30 * time.Millisecond)

	ok, err := a.Ask(context.Background(), capability.Request{Capability: "cap", Value: "v"}, capability.Decision{})
	if err != nil || ok {
		t.Fatalf("Ask = (%v, %v), want (false, nil) on timeout", ok, err)
	}
}

func TestAskReturnsTrueAfterResolveAllow(t *testing.T) {
	a := New(nil)
	a.store.SetHoldFor(5 * time.Second)

	done := make(chan askResult, 1)
	go func() {
		ok, err := a.Ask(context.Background(), capability.Request{Capability: "cap", Value: "v"}, capability.Decision{})
		done <- askResult{ok, err}
	}()

	waitUntil(t, func() bool { return len(a.Pending()) == 1 })
	id := a.Pending()[0].ID
	if err := a.Resolve(id, "allow"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	select {
	case got := <-done:
		if !got.ok || got.err != nil {
			t.Fatalf("Ask = (%v, %v), want (true, nil)", got.ok, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ask never returned")
	}
}

func TestAskReturnsFalseAfterResolveDeny(t *testing.T) {
	a := New(nil)
	a.store.SetHoldFor(5 * time.Second)

	done := make(chan askResult, 1)
	go func() {
		ok, err := a.Ask(context.Background(), capability.Request{Capability: "cap", Value: "v"}, capability.Decision{})
		done <- askResult{ok, err}
	}()

	waitUntil(t, func() bool { return len(a.Pending()) == 1 })
	id := a.Pending()[0].ID
	if err := a.Resolve(id, "deny"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	select {
	case got := <-done:
		if got.ok || got.err != nil {
			t.Fatalf("Ask = (%v, %v), want (false, nil)", got.ok, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ask never returned")
	}
}

func TestAskEndsWhenContextIsCancelled(t *testing.T) {
	a := New(nil)
	a.store.SetHoldFor(5 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan askResult, 1)
	go func() {
		ok, err := a.Ask(ctx, capability.Request{Capability: "cap", Value: "v"}, capability.Decision{})
		done <- askResult{ok, err}
	}()

	waitUntil(t, func() bool { return len(a.Pending()) == 1 })
	cancel()

	select {
	case got := <-done:
		if got.ok || got.err != nil {
			t.Fatalf("Ask = (%v, %v), want (false, nil) on ctx cancellation", got.ok, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ask did not return after ctx was cancelled")
	}
}

func TestPendingShowsWhatIsBeingAskedAndClearsAfter(t *testing.T) {
	a := New(nil)
	a.store.SetHoldFor(5 * time.Second)

	req := capability.Request{
		Capability: "spend.budget",
		Value:      "42.00",
		Contexts: []capability.Context{
			{Kind: "task", Ref: "t1"},
			{Kind: "global", Ref: ""},
		},
	}
	done := make(chan askResult, 1)
	go func() {
		ok, err := a.Ask(context.Background(), req, capability.Decision{Reason: "ask grant at context global decided this"})
		done <- askResult{ok, err}
	}()

	waitUntil(t, func() bool { return len(a.Pending()) == 1 })
	pending := a.Pending()[0]
	if pending.Meta.Capability != "spend.budget" || pending.Meta.Value != "42.00" {
		t.Fatalf("Pending()[0].Meta = %+v, want capability %q and value %q named", pending.Meta, "spend.budget", "42.00")
	}
	if pending.Meta.Context != "task t1" {
		t.Fatalf("Pending()[0].Meta.Context = %q, want the more specific %q", pending.Meta.Context, "task t1")
	}

	if err := a.Resolve(pending.ID, "allow"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	<-done

	if got := a.Pending(); len(got) != 0 {
		t.Fatalf("Pending after resolve = %+v, want empty", got)
	}
}

func TestResolveOnUnknownIDFails(t *testing.T) {
	a := New(nil)
	err := a.Resolve("nope", "allow")
	if !errors.Is(err, askgate.ErrNotPending) {
		t.Fatalf("Resolve(unknown) = %v, want askgate.ErrNotPending", err)
	}
}

func TestResolveRejectsInvalidDecision(t *testing.T) {
	a := New(nil)
	a.store.SetHoldFor(2 * time.Second)

	go func() {
		_, _ = a.Ask(context.Background(), capability.Request{Capability: "cap", Value: "v"}, capability.Decision{})
	}()
	waitUntil(t, func() bool { return len(a.Pending()) == 1 })
	id := a.Pending()[0].ID

	if err := a.Resolve(id, "maybe"); !errors.Is(err, ErrInvalidDecision) {
		t.Fatalf("Resolve(%q) = %v, want ErrInvalidDecision", "maybe", err)
	}
	if got := a.Pending(); len(got) != 1 {
		t.Fatalf("Pending after a rejected decision = %+v, want the ask still pending", got)
	}
	// Clean up so the test doesn't leak a goroutine blocked on the store.
	_ = a.Resolve(id, "deny")
}

func TestOnChangeFiresOnAskAndOnEnd(t *testing.T) {
	var fires atomic.Int64
	a := New(func() { fires.Add(1) })
	a.store.SetHoldFor(50 * time.Millisecond)

	a.Ask(context.Background(), capability.Request{Capability: "cap", Value: "v"}, capability.Decision{})

	if got := fires.Load(); got != 2 {
		t.Fatalf("onChange fired %d times, want 2 (start + end)", got)
	}
}

func TestConcurrentAskAndResolve(t *testing.T) {
	a := New(nil)
	a.store.SetHoldFor(3 * time.Second)
	const n = 50

	var wg sync.WaitGroup
	var allowed atomic.Int64
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := capability.Request{Capability: "cap", Value: strconv.Itoa(i)}
			ok, err := a.Ask(context.Background(), req, capability.Decision{})
			if err != nil {
				t.Errorf("Ask: %v", err)
				return
			}
			if ok {
				allowed.Add(1)
			}
		}(i)
	}

	resolverDone := make(chan struct{})
	go func() {
		defer close(resolverDone)
		resolved := 0
		deadline := time.Now().Add(3 * time.Second)
		for resolved < n && time.Now().Before(deadline) {
			for _, e := range a.Pending() {
				if err := a.Resolve(e.ID, "allow"); err == nil {
					resolved++
				}
			}
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()
	<-resolverDone

	if got := allowed.Load(); got != n {
		t.Fatalf("allowed %d of %d concurrent asks, want all allowed", got, n)
	}
	if got := a.Pending(); len(got) != 0 {
		t.Fatalf("Pending after all resolved = %+v, want empty", got)
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
