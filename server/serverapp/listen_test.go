package serverapp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// testEnv points a Listen-driven server at an ephemeral port and an in-memory
// database, so the test never touches the developer's real dashboard state.
func testEnv(t *testing.T, port int) {
	t.Helper()
	t.Setenv("DASHBOARD_PORT", strconv.Itoa(port))
	t.Setenv("DASHBOARD_HOST", "127.0.0.1")
	t.Setenv("DASHBOARD_DB_PATH", ":memory:")
	t.Setenv("DASHBOARD_JWT_SECRET", "test-secret-test-secret-test-secret-32")
	t.Setenv("DASHBOARD_HOOKS_SECRET", "test-hooks-secret-test-hooks-secret-32")
}

// A probe that binds and releases only proves the address was free an instant
// ago. Two shells launched together both pass such a probe and race through the
// whole config/plugin/database window before either binds for real, and the
// loser then adopts the winner's server. Listen has to keep the address.
func TestListenKeepsTheAddressItBound(t *testing.T) {
	port := freePort(t)
	testEnv(t, port)

	inst, err := Listen("")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer inst.Close() //nolint:errcheck

	if want := fmt.Sprintf("127.0.0.1:%d", port); inst.Addr() != want {
		t.Fatalf("Addr() = %q, want %q", inst.Addr(), want)
	}

	second, err := net.Listen("tcp", inst.Addr())
	if err == nil {
		second.Close() //nolint:errcheck
		t.Fatal("a second bind succeeded while Listen was supposed to be holding the address")
	}
	if _, err := Listen(""); err == nil {
		t.Fatal("a second Listen succeeded on an address the first one holds")
	}
}

// Serve must run on the listener Listen already holds. If it bound the address
// itself instead, it would collide with that listener and never serve.
func TestServeRunsOnTheHeldListener(t *testing.T) {
	port := freePort(t)
	testEnv(t, port)

	inst, err := Listen("")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)
	go func() { serveErr <- inst.Serve(ctx) }()

	healthURL := "http://" + inst.Addr() + "/api/system/health"
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(10 * time.Second)
	var served bool
	for time.Now().Before(deadline) {
		select {
		case err := <-serveErr:
			cancel()
			t.Fatalf("Serve returned before answering on the held address: %v", err)
		default:
		}
		resp, err := client.Get(healthURL)
		if err == nil {
			resp.Body.Close() //nolint:errcheck
			if resp.StatusCode == http.StatusOK {
				served = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	if !served {
		t.Fatal("the server never answered on the address Listen holds")
	}
	select {
	case err := <-serveErr:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Serve did not return after its context was cancelled")
	}
}
