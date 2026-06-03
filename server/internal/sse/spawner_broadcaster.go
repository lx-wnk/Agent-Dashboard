package sse

import "encoding/json"

// SpawnerEvent is a server-sent event for spawner CRUD. JSON keys MUST match the
// frontend SpawnerEvent contract in src/composables/useSpawners.ts.
type SpawnerEvent struct {
	Type      string `json:"type"`
	SpawnerID string `json:"spawnerId"`
	Payload   any    `json:"payload,omitempty"`
}

// SpawnerBroadcaster wraps Broadcaster with typed spawner-event publishing.
type SpawnerBroadcaster struct {
	b *Broadcaster
}

// NewSpawnerBroadcaster creates a SpawnerBroadcaster backed by the given Broadcaster.
func NewSpawnerBroadcaster(b *Broadcaster) *SpawnerBroadcaster { return &SpawnerBroadcaster{b: b} }

// Broadcast serializes the event and sends it to all SSE subscribers.
// Marshalling errors are dropped — the next reconnect/poll delivers fresh state.
func (s *SpawnerBroadcaster) Broadcast(event SpawnerEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	s.b.Broadcast(data)
}

// Subscribe returns a channel that receives raw SSE frames for spawner events.
func (s *SpawnerBroadcaster) Subscribe() chan []byte { return s.b.Subscribe() }

// Unsubscribe removes a subscriber channel.
func (s *SpawnerBroadcaster) Unsubscribe(ch chan []byte) { s.b.Unsubscribe(ch) }
