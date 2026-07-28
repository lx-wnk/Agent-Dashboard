package channel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakePtmx is an io.Writer standing in for the pty master in tests. It records
// every write to a mutex-guarded buffer and signals a channel so tests can
// wait for bytes to land without polling.
type fakePtmx struct {
	mu      sync.Mutex
	buf     []byte
	written chan struct{}
}

func newFakePtmx() *fakePtmx {
	return &fakePtmx{written: make(chan struct{}, 256)}
}

func (f *fakePtmx) Write(p []byte) (int, error) {
	f.mu.Lock()
	f.buf = append(f.buf, p...)
	f.mu.Unlock()
	select {
	case f.written <- struct{}{}:
	default:
	}
	return len(p), nil
}

func (f *fakePtmx) String() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return string(f.buf)
}

// waitWritten blocks until the buffer holds at least n bytes or the test
// times out.
func (f *fakePtmx) waitWritten(t *testing.T, n int) string {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		f.mu.Lock()
		got := len(f.buf)
		f.mu.Unlock()
		if got >= n {
			return f.String()
		}
		select {
		case <-f.written:
		case <-deadline:
			t.Fatalf("timed out waiting for %d bytes, got %q", n, f.String())
		}
	}
}

func bearer(secret string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+secret)
	return h
}

func TestBrokerWS_ReplayAndInput(t *testing.T) {
	fp := newFakePtmx()
	hub := newPtyHub(1024)
	hub.Write([]byte("SCREEN"))
	tok := newRotatingToken("secret")
	srv := httptest.NewServer(ptyMux(newPtyWriter(fp), hub, tok))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	url := "ws" + srv.URL[len("http"):] + "/ws"
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: bearer("secret"),
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read replay: %v", err)
	}
	if string(data) != "SCREEN" {
		t.Fatalf("replay = %q, want SCREEN", data)
	}

	if err := c.Write(ctx, websocket.MessageBinary, []byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := fp.waitWritten(t, 2); got != "hi" {
		t.Fatalf("ptmx got %q, want hi", got)
	}
}

func TestBrokerWS_LiveBroadcast(t *testing.T) {
	fp := newFakePtmx()
	hub := newPtyHub(1024)
	tok := newRotatingToken("secret")
	srv := httptest.NewServer(ptyMux(newPtyWriter(fp), hub, tok))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	url := "ws" + srv.URL[len("http"):] + "/ws"
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: bearer("secret"),
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	// Empty scrollback → the received frame must be a live write, proving the
	// broadcast branch (case b, ok := <-frames) forwards post-subscribe output.
	// The handler subscribes after the WS handshake completes, so retry the
	// write until one lands after the subscription is registered.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			hub.Write([]byte("LIVE"))
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
		}
	}()

	typ, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if typ != websocket.MessageBinary {
		t.Fatalf("live frame type = %v, want binary", typ)
	}
	if string(data) != "LIVE" {
		t.Fatalf("live = %q, want LIVE", data)
	}
}

func TestBrokerWS_UnauthorizedRejected(t *testing.T) {
	fp := newFakePtmx()
	hub := newPtyHub(1024)
	tok := newRotatingToken("secret")
	srv := httptest.NewServer(ptyMux(newPtyWriter(fp), hub, tok))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	url := "ws" + srv.URL[len("http"):] + "/ws"
	_, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: bearer("wrong"),
	})
	if err == nil {
		t.Fatalf("expected dial to fail for unauthorized request")
	}
}

func TestBrokerWS_ResizeMessageAppliedNotWrittenToPty(t *testing.T) {
	fp := newFakePtmx()
	hub := newPtyHub(1024)
	tok := newRotatingToken("secret")
	srv := httptest.NewServer(ptyMux(newPtyWriter(fp), hub, tok))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	url := "ws" + srv.URL[len("http"):] + "/ws"
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: bearer("secret"),
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	if err := c.Write(ctx, websocket.MessageText, []byte(`{"resize":{"cols":120,"rows":40}}`)); err != nil {
		t.Fatalf("write resize: %v", err)
	}
	if err := c.Write(ctx, websocket.MessageBinary, []byte("ok")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := fp.waitWritten(t, 2); got != "ok" {
		t.Fatalf("ptmx got %q, want %q (resize control message must not be written to the pty)", got, "ok")
	}
}
