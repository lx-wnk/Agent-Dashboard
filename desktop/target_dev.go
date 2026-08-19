//go:build darwin && dev

package main

import (
	"context"
	"os"
	"time"
)

// viteDevPortFallback is the port `task dev:desktop:wails` exports as
// VITE_DEV_PORT (mirroring vite.config.ts's server.port), kept here so a
// plain `wails dev`, run outside Task, still finds the right address.
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

// waitForFrontend polls the Vite dev server so a session started without it
// running fails with a clear message instead of opening a blank window.
func waitForFrontend(ctx context.Context, serverErr <-chan error) error {
	return waitForServer(ctx, serverErr, "http://"+viteDevServerAddr()+"/", 30*time.Second)
}
