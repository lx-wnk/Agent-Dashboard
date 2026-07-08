package agents

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/coder/websocket"

	"github.com/lx-wnk/agent-dashboard/sdk"
)

// TerminalTargetFn resolves the pty-broker port and auth token for the running
// session identified by pid. It is the seam over SpawnManager.TerminalTarget so
// the handler is testable without a real spawned session. Implementations
// return ErrNoTerminal when the session has no pty terminal to attach to.
type TerminalTargetFn func(pid int) (port int, token string, err error)

// TerminalHandler proxies GET /api/agents/{pid}/terminal: it resolves the
// pty-broker target for a live-injectable session and pipes WebSocket frames
// both ways between the browser and the broker. The browser never sees the
// broker's bearer token — it is only used for the server-side broker dial.
type TerminalHandler struct {
	getAgents GetAgentsFn
	target    TerminalTargetFn
}

// NewTerminalHandler creates a TerminalHandler.
func NewTerminalHandler(getAgents GetAgentsFn, target TerminalTargetFn) *TerminalHandler {
	return &TerminalHandler{getAgents: getAgents, target: target}
}

// Terminal handles GET /api/agents/{pid}/terminal. It sits behind the same
// JWT/Origin middleware as the other /api/agents/{pid}/* mutations, so no
// additional auth check happens here. Not wrapped in apierr.ErrorMiddleware:
// once websocket.Accept hijacks the connection, there is no response left for
// that middleware to write.
func (h *TerminalHandler) Terminal(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.Atoi(r.PathValue("pid"))
	if err != nil || pid <= 0 {
		http.Error(w, `{"error":"invalid pid"}`, http.StatusBadRequest)
		return
	}

	agentList, err := h.getAgents(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to look up agent"}`, http.StatusInternalServerError)
		return
	}
	var agent *sdk.Agent
	for i := range agentList {
		if agentList[i].PID == pid {
			agent = &agentList[i]
			break
		}
	}
	// Only spawned/injectable sessions have a broker pty to attach to.
	if agent == nil || !agent.LiveInjectable {
		http.Error(w, `{"error":"session has no terminal"}`, http.StatusConflict)
		return
	}

	port, token, err := h.target(pid)
	if err != nil {
		if errors.Is(err, ErrNoTerminal) {
			http.Error(w, `{"error":"session has no terminal"}`, http.StatusConflict)
			return
		}
		http.Error(w, `{"error":"failed to resolve terminal target"}`, http.StatusInternalServerError)
		return
	}

	// OriginPatterns is "*" here because this route already sits behind the
	// router's Origin/JWT middleware (RequireSameOriginForMutations, RequireAuth) —
	// mirrors the broker's own /ws handler (internal/channel/ptyhost.go).
	browserConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return
	}
	defer browserConn.Close(websocket.StatusInternalError, "")

	pipeCtx, cancel := context.WithCancel(r.Context())
	defer cancel()

	brokerConn, _, err := websocket.Dial(pipeCtx, fmt.Sprintf("ws://127.0.0.1:%d/ws", port), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + token}},
	})
	if err != nil {
		browserConn.Close(websocket.StatusInternalError, "failed to reach terminal broker")
		return
	}
	defer brokerConn.Close(websocket.StatusInternalError, "")

	go func() {
		defer cancel()
		pumpFrames(pipeCtx, brokerConn, browserConn)
	}()
	pumpFrames(pipeCtx, browserConn, brokerConn)
}

// pumpFrames copies WebSocket frames from src to dst, preserving message type
// (binary/text), until ctx is cancelled or either side errors. The caller is
// responsible for cancelling ctx and closing both connections on return so the
// peer pump (running in the sibling goroutine) unblocks and exits too.
func pumpFrames(ctx context.Context, src, dst *websocket.Conn) {
	for {
		typ, data, err := src.Read(ctx)
		if err != nil {
			return
		}
		if err := dst.Write(ctx, typ, data); err != nil {
			return
		}
	}
}
