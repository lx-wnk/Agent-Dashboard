package channel

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// defaultInjectTokenRotateMs is the discovery-token rotation interval used when
// DASHBOARD_INJECT_TOKEN_ROTATE_MS is unset. Explicitly setting the env var to a
// non-positive value disables rotation.
const defaultInjectTokenRotateMs = 300_000

// rotatingToken holds the active bearer token for a live-injection HTTP endpoint
// and, for one rotation interval after a rotation, the immediately-preceding
// token. A request authenticates against either, so a delivery that read the
// discovery file just before a rotation still succeeds (grace window).
type rotatingToken struct {
	current  atomic.Pointer[string]
	previous atomic.Pointer[string]
}

func newRotatingToken(initial string) *rotatingToken {
	t := &rotatingToken{}
	t.current.Store(&initial)
	return t
}

// value returns the current token string (the one written to the discovery file).
func (t *rotatingToken) value() string {
	if p := t.current.Load(); p != nil {
		return *p
	}
	return ""
}

// rotate generates a fresh token, demotes the current token to the grace slot,
// and returns the new current token. The previous grace token is discarded.
func (t *rotatingToken) rotate() (string, error) {
	next, err := generateToken()
	if err != nil {
		return "", err
	}
	prev := t.current.Load()
	t.current.Store(&next)
	t.previous.Store(prev)
	return next, nil
}

// authorize reports whether the request's bearer token matches the current or
// grace token, using a constant-time comparison.
func (t *rotatingToken) authorize(r *http.Request) bool {
	got := []byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if cur := t.current.Load(); cur != nil && subtle.ConstantTimeCompare(got, []byte(*cur)) == 1 {
		return true
	}
	if prev := t.previous.Load(); prev != nil && subtle.ConstantTimeCompare(got, []byte(*prev)) == 1 {
		return true
	}
	return false
}

// injectTokenRotateInterval resolves the rotation interval from
// DASHBOARD_INJECT_TOKEN_ROTATE_MS. Unset → defaultInjectTokenRotateMs.
// A set, non-positive, or unparseable value → 0 (rotation disabled).
func injectTokenRotateInterval() time.Duration {
	raw, ok := os.LookupEnv("DASHBOARD_INJECT_TOKEN_ROTATE_MS")
	if !ok {
		return defaultInjectTokenRotateMs * time.Millisecond
	}
	ms, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

// startTokenRotation rotates tok every interval and calls rewrite with the new
// token so callers can re-emit the discovery file at 0600. It returns
// immediately when interval <= 0 (rotation disabled). Blocks until ctx is done.
func startTokenRotation(ctx context.Context, tok *rotatingToken, interval time.Duration, rewrite func(newToken string) error) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			next, err := tok.rotate()
			if err != nil {
				slog.Warn("channel: inject token rotate failed", "err", err)
				continue
			}
			if rewrite != nil {
				if err := rewrite(next); err != nil {
					slog.Warn("channel: discovery rewrite after rotation failed", "err", err)
				}
			}
		}
	}
}
