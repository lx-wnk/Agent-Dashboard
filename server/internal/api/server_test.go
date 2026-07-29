package api_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/api"
)

// freePort asks the OS for an unused loopback port and immediately releases
// it. A subsequent bind can, in principle, race another process for the same
// port, but this is the standard Go test idiom for picking an ephemeral port.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestServer_Run_ShutsDownWhileSSEStreamOpen reproduces the desktop-quit hang:
// a streaming handler that only observes r.Context().Done() must unblock and
// let Shutdown return promptly once the Run context is cancelled, instead of
// burning the full shutdown timeout because net/http.Server.Shutdown never
// cancels in-flight request contexts on its own.
func TestServer_Run_ShutsDownWhileSSEStreamOpen(t *testing.T) {
	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	handlerStarted := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("x")); err != nil {
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		close(handlerStarted)
		<-r.Context().Done()
	})

	const shutdownTimeout = 10 * time.Second
	srv := api.NewServer(addr, handler, shutdownTimeout)

	ctx, cancel := context.WithCancel(context.Background())
	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- srv.Run(ctx)
	}()

	client := &http.Client{Timeout: 0}
	var resp *http.Response
	deadline := time.Now().Add(5 * time.Second)
	for {
		var err error
		resp, err = client.Get("http://" + addr + "/stream")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("server never became reachable: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	firstByte, err := reader.ReadByte()
	if err != nil {
		cancel()
		t.Fatalf("reading first byte from stream: %v", err)
	}
	if firstByte != 'x' {
		cancel()
		t.Fatalf("unexpected first byte: %q", firstByte)
	}

	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("handler never signalled it started streaming")
	}

	start := time.Now()
	cancel()

	select {
	case err := <-runErrCh:
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("Run returned error after %v: %v", elapsed, err)
		}
		if elapsed >= 2*time.Second {
			t.Fatalf("Run took %v to return, expected well under the %v shutdown timeout", elapsed, shutdownTimeout)
		}
		t.Logf("Run returned nil after %v", elapsed)
	case <-time.After(shutdownTimeout + 2*time.Second):
		t.Fatal("Run did not return even after the shutdown timeout elapsed")
	}
}
