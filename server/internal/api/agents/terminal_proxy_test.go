package agents

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/sdk"
)

// fakeBroker stands in for the pty-broker's /ws endpoint: it sends a hello
// frame on connect, then echoes every subsequent frame it receives.
func fakeBroker(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusInternalError, "")
		ctx := r.Context()

		if err := c.Write(ctx, websocket.MessageBinary, []byte("HELLO")); err != nil {
			return
		}
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if err := c.Write(ctx, typ, data); err != nil {
				return
			}
		}
	})
	return httptest.NewServer(mux)
}

func brokerPort(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	u := strings.TrimPrefix(srv.URL, "http://127.0.0.1:")
	u = strings.TrimPrefix(u, "http://localhost:")
	port, err := strconv.Atoi(u)
	require.NoError(t, err)
	return port
}

func mountTerminalHandler(h *TerminalHandler) *httptest.Server {
	r := chi.NewRouter()
	r.Get("/api/agents/{pid}/terminal", h.Terminal)
	return httptest.NewServer(r)
}

func TestTerminalProxy_BridgesFramesBothWays(t *testing.T) {
	broker := fakeBroker(t)
	defer broker.Close()
	port := brokerPort(t, broker)

	getAgents := func(context.Context) ([]sdk.Agent, error) {
		return []sdk.Agent{{PID: 4242, LiveInjectable: true}}, nil
	}
	target := func(pid int) (int, string, error) { return port, "tok", nil }

	h := NewTerminalHandler(getAgents, target)
	srv := mountTerminalHandler(h)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/agents/4242/terminal"
	c, _, err := websocket.Dial(ctx, url, nil)
	require.NoError(t, err)
	defer c.Close(websocket.StatusNormalClosure, "")

	typ, data, err := c.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, websocket.MessageBinary, typ)
	require.Equal(t, "HELLO", string(data))

	require.NoError(t, c.Write(ctx, websocket.MessageBinary, []byte("x")))
	typ, data, err = c.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, websocket.MessageBinary, typ)
	require.Equal(t, "x", string(data))
}

func TestTerminalProxy_NoTerminal_Returns409(t *testing.T) {
	getAgents := func(context.Context) ([]sdk.Agent, error) {
		return []sdk.Agent{{PID: 4243, LiveInjectable: true}}, nil
	}
	target := func(pid int) (int, string, error) { return 0, "", ErrNoTerminal }

	h := NewTerminalHandler(getAgents, target)
	srv := mountTerminalHandler(h)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/agents/4243/terminal"
	_, resp, err := websocket.Dial(ctx, url, nil)
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestTerminalProxy_AgentNotInjectable_Returns409(t *testing.T) {
	getAgents := func(context.Context) ([]sdk.Agent, error) {
		return []sdk.Agent{{PID: 4244, LiveInjectable: false}}, nil
	}
	target := func(pid int) (int, string, error) {
		t.Fatalf("target should not be called for a non-injectable agent")
		return 0, "", nil
	}

	h := NewTerminalHandler(getAgents, target)
	srv := mountTerminalHandler(h)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/agents/4244/terminal"
	_, resp, err := websocket.Dial(ctx, url, nil)
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestTerminalProxy_UnknownAgent_Returns409(t *testing.T) {
	getAgents := func(context.Context) ([]sdk.Agent, error) { return nil, nil }
	target := func(pid int) (int, string, error) {
		t.Fatalf("target should not be called for an unknown agent")
		return 0, "", nil
	}

	h := NewTerminalHandler(getAgents, target)
	srv := mountTerminalHandler(h)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/agents/9999/terminal"
	_, resp, err := websocket.Dial(ctx, url, nil)
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestTerminalProxy_InvalidPID_Returns400(t *testing.T) {
	getAgents := func(context.Context) ([]sdk.Agent, error) { return nil, nil }
	target := func(pid int) (int, string, error) {
		t.Fatalf("target should not be called for an invalid pid")
		return 0, "", nil
	}

	h := NewTerminalHandler(getAgents, target)
	srv := mountTerminalHandler(h)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/agents/not-a-pid/terminal"
	_, resp, err := websocket.Dial(ctx, url, nil)
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
