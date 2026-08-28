// Package askgate holds a call open until an out-of-band answer arrives, or
// the wait times out, or the caller's own context ends.
//
// It is deliberately posture-neutral: Ask reports only whether a decision
// arrived, never why not, and never what "no decision" should mean to the
// caller. Two different enforcement points can hold the same kind of call and
// disagree on what a timeout means (proceed vs refuse) without either one
// living in this package.
package askgate

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrNotPending means id names no request currently waiting for a decision —
// it was never registered, already resolved, or already lapsed.
var ErrNotPending = errors.New("askgate: not pending")

// Entry is one pending request, as returned by List.
type Entry[T any] struct {
	ID   string
	Meta T
}

type held[T any] struct {
	meta T
	ch   chan string
}

// Store holds requests open while callers wait for an answer. The zero value
// is not usable — construct with New.
//
// There is no cap on how many requests may be pending at once, on purpose: a
// cap protects a caller that can fall back to something else when refused,
// but a store that refuses new entries would instead deny a legitimate
// request just because others are already waiting. Callers that need their
// own cap enforce it themselves before calling Ask.
type Store[T any] struct {
	mu       sync.Mutex
	pending  map[string]*held[T]
	holdFor  time.Duration
	onChange func()
}

// New returns a Store whose Ask waits up to holdFor for a decision. onChange,
// which may be nil, is called whenever the pending set changes.
func New[T any](holdFor time.Duration, onChange func()) *Store[T] {
	return &Store[T]{
		pending:  make(map[string]*held[T]),
		holdFor:  holdFor,
		onChange: onChange,
	}
}

// SetHoldFor changes how long a future Ask waits. A call already blocked in
// Ask keeps the duration it started with.
func (s *Store[T]) SetHoldFor(d time.Duration) {
	s.mu.Lock()
	s.holdFor = d
	s.mu.Unlock()
}

// Ask registers id and meta as pending and blocks until Resolve delivers a
// decision, the hold times out, or ctx ends.
//
// ok is false for every way of getting no decision — timeout or ctx.Done —
// and callers map that outcome to their own fallback; this package has none.
func (s *Store[T]) Ask(ctx context.Context, id string, meta T) (decision string, ok bool) {
	ch := make(chan string, 1)
	s.mu.Lock()
	holdFor := s.holdFor
	s.pending[id] = &held[T]{meta: meta, ch: ch}
	s.mu.Unlock()
	s.changed()

	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		s.changed()
	}()

	select {
	case decision := <-ch:
		return decision, true
	case <-time.After(holdFor):
		return "", false
	case <-ctx.Done():
		return "", false
	}
}

// Resolve delivers a decision to the request named by id.
//
// decide runs against the request's own metadata while the request is still
// locked in place, atomically with the look-up and the removal: splitting
// that into a separate look-up let a hold lapse in the gap, so the decision
// still went out to a caller that had already given up. Returning a non-nil
// error vetoes delivery and leaves the request pending.
func (s *Store[T]) Resolve(id string, decide func(meta T) (decision string, err error)) error {
	s.mu.Lock()
	e, ok := s.pending[id]
	if !ok {
		s.mu.Unlock()
		return ErrNotPending
	}
	decision, err := decide(e.meta)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	delete(s.pending, id)
	s.mu.Unlock()
	// Cap-1 buffer, sole sender, entry now unreachable to any other resolver.
	e.ch <- decision
	return nil
}

// List returns a snapshot of every request currently pending, in no defined
// order.
func (s *Store[T]) List() []Entry[T] {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry[T], 0, len(s.pending))
	for id, e := range s.pending {
		out = append(out, Entry[T]{ID: id, Meta: e.meta})
	}
	return out
}

// SetOnChange installs the callback fired whenever the pending set changes.
// A store is often built before the thing that wants to observe it exists.
func (s *Store[T]) SetOnChange(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = fn
}

func (s *Store[T]) changed() {
	s.mu.Lock()
	fn := s.onChange
	s.mu.Unlock()
	// Called outside the lock: a callback that reads this store would deadlock.
	if fn != nil {
		fn()
	}
}
