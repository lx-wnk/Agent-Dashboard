package agents

import (
	"sync"
	"time"
)

// slidingWindowLimiter is a per-key sliding-window rate limiter.
// Each key (e.g. JWT sub) gets an independent window tracked in memory.
type slidingWindowLimiter struct {
	max    int
	window time.Duration

	mu       sync.Mutex
	attempts map[string][]time.Time
}

// newSlidingWindowLimiter creates a limiter. Non-positive max or window is
// clamped to the provided defaults.
func newSlidingWindowLimiter(max int, window time.Duration, defaultMax int, defaultWindow time.Duration) *slidingWindowLimiter {
	if max <= 0 {
		max = defaultMax
	}
	if window <= 0 {
		window = defaultWindow
	}
	return &slidingWindowLimiter{
		max:      max,
		window:   window,
		attempts: make(map[string][]time.Time),
	}
}

// Allow reports whether a new attempt is permitted for key. It prunes stale
// entries but does NOT record an attempt — call Record after Allow returns true.
func (l *slidingWindowLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(key)
	return len(l.attempts[key]) < l.max
}

// Record appends a timestamp for key. Caller must have verified Allow first.
func (l *slidingWindowLimiter) Record(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(key)
	l.attempts[key] = append(l.attempts[key], time.Now())
}

// AllowAndRecord atomically checks the limit and records an attempt under a
// single lock hold. It records (and returns true) only when the attempt is
// permitted, closing the check-then-record race that lets concurrent callers
// exceed max. Returns false without recording when the key is at the limit.
func (l *slidingWindowLimiter) AllowAndRecord(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(key)
	if len(l.attempts[key]) >= l.max {
		return false
	}
	l.attempts[key] = append(l.attempts[key], time.Now())
	return true
}

// prune removes attempts older than the window. Caller must hold l.mu.
func (l *slidingWindowLimiter) prune(key string) {
	cutoff := time.Now().Add(-l.window)
	attempts := l.attempts[key]
	i := 0
	for i < len(attempts) && attempts[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		l.attempts[key] = attempts[i:]
	}
}

// pruneAll evicts all keys whose every attempt predates now-window.
// Called by the background pruner goroutine.
func (l *slidingWindowLimiter) pruneAll(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	for key, attempts := range l.attempts {
		i := 0
		for i < len(attempts) && attempts[i].Before(cutoff) {
			i++
		}
		if i == len(attempts) {
			delete(l.attempts, key)
		} else if i > 0 {
			l.attempts[key] = attempts[i:]
		}
	}
}
