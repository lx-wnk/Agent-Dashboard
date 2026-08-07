//go:build darwin

// Command desktop is the macOS wails shell for the Agent Dashboard. It starts
// the dashboard HTTP server in-process on 127.0.0.1:13120 and opens a webview
// that redirects to it, so the page runs on the loopback-http origin the
// server's same-origin mutation guard accepts.
package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"github.com/lx-wnk/agent-dashboard/server/serverapp"
)

//go:embed bootstrap
var bootstrapDir embed.FS

const (
	serverAddr = "127.0.0.1:13120"
	healthURL  = "http://" + serverAddr + "/api/system/health"

	// drainTimeout is a last-resort backstop against an unquittable app, not a
	// drain budget: it must stay above the server's own shutdown.timeoutSeconds
	// (default 10s, user-configurable) so the server's own graceful path always
	// gets to finish first. If shutdown.timeoutSeconds is ever raised above this
	// value, the app can again exit mid-drain before the server closes cleanly.
	drainTimeout = 20 * time.Second
)

func main() {
	// A re-executed subcommand (pty-host, channel) must never boot a second
	// server or contend for port 13120 — dispatch and return before anything
	// else starts.
	if handled, err := serverapp.DispatchHeadless(context.Background(), os.Args[1:]); handled {
		if err != nil {
			log.Fatalf("%v", err)
		}
		return
	}

	if err := claimAddr(serverAddr); err != nil {
		log.Fatalf("%v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start the dashboard server in-process; it drains when ctx is cancelled.
	serverErr := make(chan error, 1)
	go func() { serverErr <- serverapp.Serve(ctx, "") }()

	// The bootstrap page redirects to the server, so the window must not open
	// until the server accepts requests.
	if err := waitForServer(ctx, serverErr, healthURL, 30*time.Second); err != nil {
		cancel()
		log.Fatalf("dashboard server did not become ready: %v", err)
	}

	assets, err := fs.Sub(bootstrapDir, "bootstrap")
	if err != nil {
		cancel()
		log.Fatalf("bootstrap assets: %v", err)
	}

	err = wails.Run(&options.App{
		Title:  "Agent Dashboard",
		Width:  1400,
		Height: 900,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		Mac: &mac.Options{},
		OnShutdown: func(_ context.Context) {
			cancel() // ask the in-process server to drain
			// A handler that ignores its context would otherwise make the app
			// unquittable, so the drain wait is bounded.
			select {
			case <-serverErr:
			case <-time.After(drainTimeout):
				log.Printf("dashboard server did not drain within %s, exiting anyway", drainTimeout)
			}
		},
	})
	if err != nil {
		cancel()
		log.Fatalf("wails run: %v", err)
	}
}

var healthPollClient = &http.Client{Timeout: 2 * time.Second}

// waitForServer polls url until it returns 200, the timeout/ctx elapses, or
// serverErr delivers a startup failure.
func waitForServer(ctx context.Context, serverErr <-chan error, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-serverErr:
			return fmt.Errorf("server failed to start: %w", err)
		default:
		}
		resp, err := healthPollClient.Get(url)
		if err == nil {
			resp.Body.Close()
			// A 200 only proves something serves this address, not that it is
			// ours: an already-running instance answers before our own Serve has
			// finished loading config, plugins, and the DB, so its error never
			// reaches serverErr in time. claimAddr, not this poll, is what keeps
			// a second shell from adopting the first one's server.
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout after %s waiting for %s", timeout, url)
}

// claimAddr fails when another process already listens on addr. Without it the
// shell starts, loses the port, and then adopts the running instance's server —
// opening a window on that instance's embedded SPA, so a freshly built app shows
// the previous build's frontend.
func claimAddr(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("another Agent Dashboard is already using %s — quit it first: %w", addr, err)
	}
	return ln.Close()
}
