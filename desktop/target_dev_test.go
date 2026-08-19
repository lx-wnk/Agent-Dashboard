//go:build darwin && dev

package main

import "testing"

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

	t.Run("falls back when VITE_DEV_PORT is unset", func(t *testing.T) {
		t.Setenv("VITE_DEV_PORT", "")
		// Asserted against the literal, not viteDevPortFallback: comparing the
		// fallback constant to itself would pass even if the constant drifted
		// from vite.config.ts's default.
		if got, want := frontendAddr("127.0.0.1:13120"), "127.0.0.1:5173"; got != want {
			t.Fatalf("frontendAddr() = %q, want %q (the Vite dev server)", got, want)
		}
	})
}
