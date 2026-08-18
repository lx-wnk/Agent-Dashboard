//go:build darwin && dev

package main

import "testing"

// Under `wails dev`'s `dev` tag, the redirect must go to the Vite dev
// server — not to the in-process server's own address, which still serves
// the SPA `build:frontend` embedded once before this process started.
func TestFrontendAddrTargetsViteUnderDev(t *testing.T) {
	if got, want := frontendAddr("127.0.0.1:13120"), viteDevServerAddr; got != want {
		t.Fatalf("frontendAddr() = %q, want %q (the Vite dev server)", got, want)
	}
}
