// Package sse provides an SSE broadcaster for pushing real-time updates to clients.
package sse

import "sync"

const subscriberBufferSize = 10

// Broadcaster distributes byte slices to all active SSE subscribers.
// Sends are non-blocking: if a subscriber's buffer is full, the frame is
// dropped silently and the subscriber catches up on the next broadcast.
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

// Broadcast sends data to all subscribers. Non-blocking: frames are dropped
// for slow consumers rather than blocking the broadcaster goroutine.
func (b *Broadcaster) Broadcast(data []byte) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subscribers {
		select {
		case ch <- data:
		default: // subscriber buffer full — drop frame
		}
	}
}

// SubscriberCount returns the number of active subscribers.
func (b *Broadcaster) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
