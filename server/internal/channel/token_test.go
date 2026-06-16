package channel

import (
	"net/http"
	"os"
	"testing"
	"time"
)

func authReq(t *testing.T, token string) *http.Request {
	t.Helper()
	r, err := http.NewRequest("POST", "http://127.0.0.1/message", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func TestRotatingToken_AcceptsCurrentRejectsUnknown(t *testing.T) {
	tok := newRotatingToken("alpha")
	if !tok.authorize(authReq(t, "alpha")) {
		t.Fatal("current token must be accepted")
	}
	if tok.authorize(authReq(t, "bogus")) {
		t.Fatal("unknown token must be rejected")
	}
	if tok.authorize(authReq(t, "")) {
		t.Fatal("empty token must be rejected")
	}
}

func TestRotatingToken_RotateSwapsAndKeepsGrace(t *testing.T) {
	tok := newRotatingToken("alpha")

	next, err := tok.rotate()
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if next == "alpha" || next == "" {
		t.Fatalf("rotate produced unexpected token %q", next)
	}
	if tok.value() != next {
		t.Fatalf("value() = %q, want %q", tok.value(), next)
	}
	// New token accepted; previous token still accepted during the grace window.
	if !tok.authorize(authReq(t, next)) {
		t.Fatal("new token must be accepted after rotation")
	}
	if !tok.authorize(authReq(t, "alpha")) {
		t.Fatal("previous token must be accepted during grace window")
	}
}

func TestRotatingToken_SecondRotationEvictsOldestToken(t *testing.T) {
	tok := newRotatingToken("alpha")
	beta, _ := tok.rotate()
	gamma, _ := tok.rotate()

	if !tok.authorize(authReq(t, gamma)) {
		t.Fatal("newest token must be accepted")
	}
	if !tok.authorize(authReq(t, beta)) {
		t.Fatal("immediately-previous token must remain in grace")
	}
	if tok.authorize(authReq(t, "alpha")) {
		t.Fatal("two-generations-old token must be rejected")
	}
}

func TestInjectTokenRotateInterval(t *testing.T) {
	t.Run("unset defaults on", func(t *testing.T) {
		prev, had := os.LookupEnv("DASHBOARD_INJECT_TOKEN_ROTATE_MS")
		_ = os.Unsetenv("DASHBOARD_INJECT_TOKEN_ROTATE_MS")
		t.Cleanup(func() {
			if had {
				_ = os.Setenv("DASHBOARD_INJECT_TOKEN_ROTATE_MS", prev)
			}
		})
		if got := injectTokenRotateInterval(); got != defaultInjectTokenRotateMs*time.Millisecond {
			t.Fatalf("unset: got %v, want %v", got, defaultInjectTokenRotateMs*time.Millisecond)
		}
	})
	t.Run("positive value honored", func(t *testing.T) {
		t.Setenv("DASHBOARD_INJECT_TOKEN_ROTATE_MS", "1500")
		if got := injectTokenRotateInterval(); got != 1500*time.Millisecond {
			t.Fatalf("got %v, want 1.5s", got)
		}
	})
	t.Run("zero disables", func(t *testing.T) {
		t.Setenv("DASHBOARD_INJECT_TOKEN_ROTATE_MS", "0")
		if got := injectTokenRotateInterval(); got != 0 {
			t.Fatalf("zero: got %v, want 0", got)
		}
	})
	t.Run("garbage disables", func(t *testing.T) {
		t.Setenv("DASHBOARD_INJECT_TOKEN_ROTATE_MS", "not-a-number")
		if got := injectTokenRotateInterval(); got != 0 {
			t.Fatalf("garbage: got %v, want 0", got)
		}
	})
}
