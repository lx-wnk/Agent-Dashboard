package mcp

import (
	"encoding/json"
	"log/slog"
	"sync"
)

// Notifier fans out MCP notifications to SSE subscribers. Subscribe returns a
// channel that receives pre-encoded SSE data frames; the caller is responsible
// for writing them to the ResponseWriter. Unsubscribe removes the channel.
type Notifier struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

// NewNotifier constructs a Notifier.
func NewNotifier() *Notifier {
	return &Notifier{subs: make(map[chan []byte]struct{})}
}

// Subscribe returns a buffered channel that receives SSE data frames, and an
// unsubscribe function the caller must invoke when the connection closes.
func (n *Notifier) Subscribe() (ch <-chan []byte, unsub func()) {
	c := make(chan []byte, 4)
	n.mu.Lock()
	n.subs[c] = struct{}{}
	n.mu.Unlock()
	return c, func() {
		n.mu.Lock()
		delete(n.subs, c)
		n.mu.Unlock()
	}
}

// NotifyToolsChanged broadcasts a notifications/tools/list_changed JSON-RPC
// notification to every subscriber as an SSE data frame. A slow or full
// subscriber channel is skipped (non-blocking send) and logged at debug level.
func (n *Notifier) NotifyToolsChanged() {
	msg, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/tools/list_changed",
	})
	if err != nil {
		slog.Error("mcp: marshal tools/list_changed", "err", err)
		return
	}
	frame := append([]byte("data: "), msg...)
	frame = append(frame, '\n', '\n')

	n.mu.Lock()
	defer n.mu.Unlock()
	for ch := range n.subs {
		select {
		case ch <- frame:
		default:
			slog.Debug("mcp: tools/list_changed dropped for slow subscriber")
		}
	}
}
