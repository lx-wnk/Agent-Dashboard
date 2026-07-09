package channel

import "sync"

// scrollback is a fixed-size ring buffer of the most recent pty output bytes,
// replayed to a client on connect so the current screen (and any open modal) is
// visible immediately.
type scrollback struct {
	mu   sync.Mutex
	buf  []byte
	size int
}

func newScrollback(size int) *scrollback { return &scrollback{size: size} }

func (s *scrollback) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, p...)
	if len(s.buf) > s.size {
		s.buf = append([]byte(nil), s.buf[len(s.buf)-s.size:]...)
	}
	return len(p), nil
}

func (s *scrollback) Snapshot() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.buf...)
}
