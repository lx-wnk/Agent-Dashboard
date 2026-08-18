//go:build darwin && !dev

package main

import "testing"

// Outside the `dev` build tag — every non-`wails dev` build, including
// production — the redirect targets the in-process server's own address.
func TestFrontendAddrTargetsTheInProcessServerOutsideDev(t *testing.T) {
	if got, want := frontendAddr("127.0.0.1:13120"), "127.0.0.1:13120"; got != want {
		t.Fatalf("frontendAddr() = %q, want %q", got, want)
	}
}
