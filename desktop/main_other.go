//go:build !darwin

// The desktop shell wraps a macOS WKWebView; it is not built on other
// platforms. Non-macOS users run the server directly via `agent-dashboard
// serve`. This stub keeps the package compilable everywhere so `go build
// ./...` and CI stay green off darwin.
package main

import "log"

func main() {
	log.Fatal("the Agent Dashboard desktop shell is only supported on macOS; run `agent-dashboard serve` instead")
}
