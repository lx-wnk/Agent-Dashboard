//go:build darwin

package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/serverapp"
)

// freePort picks an unused loopback port and releases it, so the test can watch
// the shell claim it.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close() //nolint:errcheck
	return ln.Addr().(*net.TCPAddr).Port
}

// dashboardEnv points the shell's server at an ephemeral port and an in-memory
// database, so a test never touches the developer's real dashboard state.
func dashboardEnv(t *testing.T, port int) {
	t.Helper()
	t.Setenv("DASHBOARD_PORT", strconv.Itoa(port))
	t.Setenv("DASHBOARD_HOST", "127.0.0.1")
	t.Setenv("DASHBOARD_DB_PATH", ":memory:")
	t.Setenv("DASHBOARD_JWT_SECRET", "test-secret-test-secret-test-secret-32")
	t.Setenv("DASHBOARD_HOOKS_SECRET", "test-hooks-secret-test-hooks-secret-32")
}

// Two `open -n` launches race through the whole config/plugin/database startup
// window before either binds for real. Only holding the address from the moment
// the shell claims it until the server serves on it makes the second launch
// lose — a probe that releases lets both pass, and the loser then adopts the
// winner's server.
func TestTheClaimedAddressIsHeldNotJustProbed(t *testing.T) {
	dashboardEnv(t, freePort(t))

	instance, err := serverapp.Listen("")
	if err != nil {
		t.Fatalf("first claim on a free address: %v", err)
	}
	defer instance.Close() //nolint:errcheck

	second, err := net.Listen("tcp", instance.Addr())
	if err == nil {
		second.Close() //nolint:errcheck
		t.Fatal("a second launch bound the address the first one had already claimed")
	}
}

func TestTheSecondLaunchIsRejectedWhileTheFirstHoldsTheAddress(t *testing.T) {
	dashboardEnv(t, freePort(t))

	first, err := serverapp.Listen("")
	if err != nil {
		t.Fatalf("first launch: %v", err)
	}
	defer first.Close() //nolint:errcheck

	if second, err := serverapp.Listen(""); err == nil {
		second.Close() //nolint:errcheck
		t.Fatal("a second shell claimed an address the first one is holding")
	}
}

// waitForServer cannot tell our server from someone else's on the same address:
// a running instance answers 200 long before our own server reaches its
// listener, so its bind error never arrives in time to be noticed. That is why
// the address must already be held before this runs — it is not itself a check.
func TestWaitForServerCannotTellAForeignServerFromOurOwn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := waitForServer(context.Background(), make(chan error, 1), srv.URL, 2*time.Second); err != nil {
		t.Fatalf("waitForServer on a foreign but healthy server: %v", err)
	}
}

func TestWaitForServerReportsAStartupFailureThatArrivesFirst(t *testing.T) {
	serverErr := make(chan error, 1)
	serverErr <- errors.New("listen: address already in use")

	// Nothing serves this address, so a timeout would also produce an error —
	// assert the text so deleting the serverErr arm cannot leave this green.
	err := waitForServer(context.Background(), serverErr, "http://127.0.0.1:1/health", 2*time.Second)
	if err == nil {
		t.Fatal("waitForServer ignored a startup failure")
	}
	if !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("want the startup failure, got %v", err)
	}
}

// The window must be sent to the address the server really bound. A hardcoded
// port in the bootstrap page sends it to one nothing listens on as soon as the
// configured port differs.
func TestBootstrapPageRedirectsToTheAddressTheServerBound(t *testing.T) {
	rec := httptest.NewRecorder()
	bootstrapHandler("http://127.0.0.1:19999/?shell=desktop").
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	if !strings.Contains(body, "window.location.replace('http://127.0.0.1:19999/?shell=desktop')") {
		t.Fatalf("bootstrap page does not redirect to the bound address:\n%s", body)
	}
	if strings.Contains(body, dashboardURLPlaceholder) {
		t.Fatal("the placeholder was served unsubstituted")
	}
}
