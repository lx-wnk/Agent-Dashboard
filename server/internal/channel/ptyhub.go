package channel

import "sync"

// ptyHub tees pty output into a scrollback buffer and to all live subscribers.
// A subscriber that cannot keep up is dropped (its channel closed) rather than
// stalling the pty read loop.
type ptyHub struct {
	mu   sync.Mutex
	sb   *scrollback
	subs map[chan []byte]struct{}
}

func newPtyHub(scrollbackBytes int) *ptyHub {
	return &ptyHub{sb: newScrollback(scrollbackBytes), subs: map[chan []byte]struct{}{}}
}

// Write is io.Writer: called from the pty read loop.
func (h *ptyHub) Write(p []byte) (int, error) {
	h.sb.Write(p)
	h.mu.Lock()
	for ch := range h.subs {
		b := append([]byte(nil), p...)
		select {
		case ch <- b:
		default: // slow subscriber: drop it
			close(ch)
			delete(h.subs, ch)
		}
	}
	h.mu.Unlock()
	return len(p), nil
}

// Subscribe returns the current scrollback plus a channel of subsequent frames.
func (h *ptyHub) Subscribe() (replay []byte, frames chan []byte, cancel func()) {
	ch := make(chan []byte, 256)
	h.mu.Lock()
	replay = h.sb.Snapshot()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	cancel = func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			close(ch)
			delete(h.subs, ch)
		}
		h.mu.Unlock()
	}
	return replay, ch, cancel
}
