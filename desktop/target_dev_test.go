//go:build darwin && dev

package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Under `wails dev`'s `dev` tag, the redirect must go to the Vite dev
// server — not to the in-process server's own address, which still serves
// the SPA `build:frontend` embedded once before this process started.
func TestFrontendAddrTargetsViteUnderDev(t *testing.T) {
	t.Run("honours VITE_DEV_PORT when set", func(t *testing.T) {
		t.Setenv("VITE_DEV_PORT", "6100")
		if got, want := frontendAddr("127.0.0.1:13120"), "127.0.0.1:6100"; got != want {
			t.Fatalf("frontendAddr() = %q, want %q (the Vite dev server)", got, want)
		}
	})

	t.Run("falls back when VITE_DEV_PORT is empty", func(t *testing.T) {
		t.Setenv("VITE_DEV_PORT", "")
		// Asserted against the literal, not viteDevPortFallback: comparing the
		// fallback constant to itself would pass even if the constant drifted
		// from vite.config.ts's default.
		if got, want := frontendAddr("127.0.0.1:13120"), "127.0.0.1:5173"; got != want {
			t.Fatalf("frontendAddr() = %q, want %q (the Vite dev server)", got, want)
		}
	})
}

// serveOnLoopback starts h on a loopback port and points VITE_DEV_PORT at it,
// so waitForFrontend polls this handler instead of a real Vite.
func serveOnLoopback(t *testing.T, h http.Handler) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := httptest.NewUnstartedServer(h)
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	t.Setenv("VITE_DEV_PORT", port)
}

// viteHandler answers the way a Vite dev server proxying to a live backend
// does. Individual tests swap out one route to construct a failure.
func viteHandler(client, health http.HandlerFunc) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/@vite/client", client)
	mux.HandleFunc("/api/system/health", health)
	return mux
}

func okVite(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript")
	_, _ = w.Write([]byte("export const x = 1\n"))
}

func okHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// waitOnce runs waitForFrontend under a short deadline: the rejection cases
// would otherwise sit out the production timeouts, and the loop checks ctx
// before each probe, so the deadline bounds the test without touching them.
func waitOnce(t *testing.T, d time.Duration) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return waitForFrontend(ctx, make(chan error))
}

func TestWaitForFrontendAcceptsAViteDevServer(t *testing.T) {
	serveOnLoopback(t, viteHandler(okVite, okHealth))
	if err := waitOnce(t, 10*time.Second); err != nil {
		t.Fatalf("waitForFrontend() = %v, want nil for a real Vite dev server", err)
	}
}

// The failure this guards: `strictPort` makes our Vite exit when the port is
// taken, and a bare 200 from whatever squatted it would make the desktop window
// open onto a foreign origin. Serving 200 on every path must not be enough.
func TestWaitForFrontendRejectsAForeignServerOnThePort(t *testing.T) {
	squatter := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>not vite</html>"))
	})
	serveOnLoopback(t, squatter)

	err := waitOnce(t, 500*time.Millisecond)
	if err == nil {
		t.Fatal("waitForFrontend() = nil, want an error: a foreign server held the port")
	}
	if !strings.Contains(err.Error(), "no Vite dev server") {
		t.Fatalf("waitForFrontend() = %v, want the error to name the missing Vite dev server", err)
	}
}

// Vite is up but its /api proxy points at a port the shell does not hold —
// the DASHBOARD_PORT divergence. The proxy swallows ECONNREFUSED, so without
// this probe the window opens onto a dashboard whose every call fails silently.
func TestWaitForFrontendRejectsADeadBackendBehindTheProxy(t *testing.T) {
	deadBackend := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}
	serveOnLoopback(t, viteHandler(okVite, deadBackend))

	err := waitOnce(t, 500*time.Millisecond)
	if err == nil {
		t.Fatal("waitForFrontend() = nil, want an error: the proxied backend did not answer")
	}
	if !strings.Contains(err.Error(), "DASHBOARD_PORT") {
		t.Fatalf("waitForFrontend() = %v, want the error to name DASHBOARD_PORT", err)
	}
}
