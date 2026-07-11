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
	"net/http"
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
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	// Start the dashboard server in-process; it drains when ctx is cancelled.
	serverErr := make(chan error, 1)
	go func() { serverErr <- serverapp.Serve(ctx, "") }()

	// The bootstrap page redirects to the server, so the window must not open
	// until the server accepts requests.
	if err := waitForServer(ctx, healthURL, 30*time.Second); err != nil {
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
			cancel()     // ask the in-process server to drain
			<-serverErr  // wait for graceful shutdown before the process exits
		},
	})
	if err != nil {
		cancel()
		log.Fatalf("wails run: %v", err)
	}
}

// waitForServer polls url until it returns 200 or the timeout/ctx elapses.
func waitForServer(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout after %s waiting for %s", timeout, url)
}
