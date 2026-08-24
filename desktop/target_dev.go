//go:build darwin && dev

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// viteDevPortFallback is the port `task dev:desktop` exports as VITE_DEV_PORT
// (mirroring vite.config.ts's server.port), kept here so a plain `wails dev`,
// run outside Task, still finds the right address.
const viteDevPortFallback = "5173"

// viteDevServerAddr is where `task dev:desktop` runs the Vite dev server.
// Its proxy forwards /api and /auth to DASHBOARD_PORT (default 13120) — the
// same address the shell's in-process server binds by default, so the two
// agree without extra wiring.
func viteDevServerAddr() string {
	port := os.Getenv("VITE_DEV_PORT")
	if port == "" {
		port = viteDevPortFallback
	}
	return "127.0.0.1:" + port
}

// frontendAddr redirects the webview to the Vite dev server instead of the
// address the in-process server bound. `wails dev` sets this build's `dev`
// tag itself, and under it the server would otherwise still serve the SPA
// `build:frontend` embedded once before this process started — frozen for
// the rest of the session.
func frontendAddr(_ string) string { return viteDevServerAddr() }

// servesViteClient reports whether the peer answering /@vite/client is OUR
// Vite dev server. When DESKTOP_DEV_NONCE is set, a matching
// x-dashboard-dev-nonce header is proof of that: `task dev:desktop` generates
// the nonce once and hands it to both this process and the Vite child (see
// Taskfile.yml), so only a process sharing that parent can echo it back.
// Without the env var (a plain `wails dev` run outside Task) it falls back to
// the JavaScript Content-Type only Vite serves here, which proves merely that
// something Vite-shaped answered, not that it is ours.
func servesViteClient(resp *http.Response) bool {
	if nonce := os.Getenv("DESKTOP_DEV_NONCE"); nonce != "" {
		return resp.Header.Get("x-dashboard-dev-nonce") == nonce
	}
	return strings.Contains(resp.Header.Get("Content-Type"), "javascript")
}

// waitForFrontend polls the Vite dev server so a session started without it
// running fails with a clear message instead of opening a blank window.
//
// It asks for /@vite/client rather than /, because this process never binds
// 5173: the ownership precondition that lets waitForServer accept a bare 200
// does not hold here, and a foreign process holding the port would otherwise
// become the desktop window's origin. strictPort only makes OUR Vite refuse to
// move off the port; it says nothing about who answers when it cannot start.
// servesViteClient closes that gap when DESKTOP_DEV_NONCE is set: a matching
// header proves the peer shares a parent with this process. Run outside Task
// (no nonce), it falls back to a Content-Type sniff, which proves only that
// something Vite-shaped answered, not that it is ours.
func waitForFrontend(ctx context.Context, serverErr <-chan error) error {
	addr := viteDevServerAddr()
	if err := waitForServerFunc(ctx, serverErr, "http://"+addr+"/@vite/client", 30*time.Second, servesViteClient); err != nil {
		return fmt.Errorf("no Vite dev server on %s: %w", addr, err)
	}
	// The window reaches the backend through Vite's proxy, whose target is
	// DASHBOARD_PORT read from process.env alone, while the shell resolved its
	// own port through koanf, a .env file and the environment. When the two
	// disagree the proxy points at a port we do not hold and every /api call
	// fails silently, because the proxy swallows ECONNREFUSED. Probe once
	// through the proxy so that surfaces as a startup error instead.
	//
	// This proves a backend answers, not that it is ours: a second dashboard on
	// the proxied port would satisfy it. Closing that needs an instance
	// identity on /api/system/health, which does not exist yet.
	if err := waitForServerFunc(ctx, serverErr, "http://"+addr+"/api/system/health", 10*time.Second, nil); err != nil {
		return fmt.Errorf("vite proxies /api to a backend that does not answer — give `task dev:desktop` the same DASHBOARD_PORT the shell resolves: %w", err)
	}
	return nil
}
