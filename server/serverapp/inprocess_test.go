package serverapp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/config"
	"github.com/lx-wnk/agent-dashboard/server/internal/restart"
)

// freePort asks the OS for an unused loopback port and immediately releases it.
// A subsequent bind can, in principle, race another process for the same port,
// but this is the standard Go test idiom for picking an ephemeral port ahead of
// time and is stable enough for a hermetic, short-lived in-process test.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestRun_InProcess_HealthMutationShutdown proves the three mechanics a future
// in-process desktop host depends on: (1) serverapp.Run boots the full server
// as a goroutine and serves traffic, (2) the loopback same-origin guard admits
// a same-Host mutating request, and (3) cancelling the caller-owned ctx
// triggers graceful shutdown and Run returns.
func TestRun_InProcess_HealthMutationShutdown(t *testing.T) {
	port := freePort(t)
	host := fmt.Sprintf("127.0.0.1:%d", port)

	cfg := config.Config{
		Host:        "127.0.0.1",
		Port:        port,
		DBPath:      ":memory:",
		RestartMode: "reexec",
		JWTSecret:   "test-secret-test-secret-test-secret-32",
		HooksSecret: "test-hooks-secret-test-hooks-secret-32",
	}

	ctx, cancel := context.WithCancel(context.Background())
	restartCtl := restart.NewController(cfg.RestartMode)

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- Run(ctx, cfg, "", restartCtl)
	}()

	healthURL := "http://" + host + "/api/system/health"
	client := &http.Client{Timeout: 2 * time.Second}

	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(healthURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				lastErr = nil
				break
			}
			lastErr = fmt.Errorf("health: unexpected status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	if lastErr != nil {
		cancel()
		t.Fatalf("server never became healthy: %v", lastErr)
	}

	// POST /api/tasks is a mutating route behind RequireSameOriginForMutations.
	// An empty body fails slug validation (400) — that's fine, the assertion is
	// only that the same-origin guard did NOT reject the request with 403.
	mutateURL := "http://" + host + "/api/tasks"
	req, err := http.NewRequest(http.MethodPost, mutateURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		cancel()
		t.Fatalf("build mutation request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://"+host)

	resp, err := client.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("mutation request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		cancel()
		t.Fatalf("same-origin guard rejected same-Host request: status=%d body=%s", resp.StatusCode, body)
	}
	t.Logf("mutation response: status=%d body=%s", resp.StatusCode, body)

	cancel()

	select {
	case err := <-runErrCh:
		if err != nil {
			t.Fatalf("Run returned error after shutdown: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return within timeout after ctx cancellation")
	}
}
