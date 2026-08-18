//go:build darwin && !dev

package main

import "context"

// frontendAddr redirects the webview to the in-process server's own
// address, which serves the SPA baked in by go:embed — unchanged from
// before the dev-desktop redirect split.
func frontendAddr(instanceAddr string) string { return instanceAddr }

// waitForFrontend is a no-op outside the dev build: the embedded SPA is
// already served by the address waitForServer already confirmed healthy.
func waitForFrontend(_ context.Context, _ <-chan error) error { return nil }
