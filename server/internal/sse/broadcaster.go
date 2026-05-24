// Package sse provides an SSE broadcaster for pushing real-time updates to clients.
package sse

import (
	"log/slog"
	"sync"
)

const subscriberBufferSize = 10

// Broadcaster distributes byte slices to all active SSE subscribers.
// Sends are non-blocking: if a subscriber's buffer is full, the frame is
// dropped and a debug log entry is emitted (F-PERF-015).
type Broadcaster struct {
	mu          sync.RWMutex
	subscribers map[chan []byte]struct{}
}

// NewBroadcaster creates a ready-to-use Broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[chan []byte]struct{}),
	}
}

// Subscribe returns a buffered channel that will receive broadcast data.
// Call Unsubscribe when done to avoid goroutine leaks.
func (b *Broadcaster) Subscribe() chan []byte {
	ch := make(chan []byte, subscriberBufferSize)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber and closes its channel.
func (b *Broadcaster) Unsubscribe(ch chan []byte) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
	close(ch)
}

// snapshot returns a point-in-time copy of all subscriber channels.
// Callers can iterate the slice without holding the lock. (F-PERF-018)
func (b *Broadcaster) snapshot() []chan []byte {
	b.mu.RLock()
	chans := make([]chan []byte, 0, len(b.subscribers))
	for ch := range b.subscribers {
		chans = append(chans, ch)
	}
	b.mu.RUnlock()
	return chans
}

// send attempts a non-blocking send of data to ch. If the channel buffer is
// full the frame is dropped and a debug message is logged. (F-PERF-015)
func send(ch chan []byte, data []byte) {
	select {
	case ch <- data:
	default:
		// subscriber buffer full — drop frame and record it for observability.
		slog.Debug("sse: dropped frame", "buffered", len(ch), "cap", cap(ch))
	}
}

// Broadcast sends data to all subscribers. Non-blocking: frames are dropped
// for slow consumers rather than blocking the broadcaster goroutine.
// Subscribers are snapshotted under RLock; iteration happens without lock. (F-PERF-018)
func (b *Broadcaster) Broadcast(data []byte) {
	for _, ch := range b.snapshot() {
		send(ch, data)
	}
}

// BroadcastComment sends an SSE comment frame (`: <text>\n\n`) to all
// subscribers. Comment frames are used as heartbeats to prevent idle
// connections from being closed by reverse-proxies. (F-PERF-006)
func (b *Broadcaster) BroadcastComment(text []byte) {
	// SSE comment format: ": <text>\n\n"
	frame := make([]byte, 0, 2+len(text)+2)
	frame = append(frame, ':', ' ')
	frame = append(frame, text...)
	frame = append(frame, '\n', '\n')
	for _, ch := range b.snapshot() {
		send(ch, frame)
	}
}

// SubscriberCount returns the number of active subscribers.
func (b *Broadcaster) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
