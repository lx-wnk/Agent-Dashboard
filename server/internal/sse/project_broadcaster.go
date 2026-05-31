package sse

import "encoding/json"

// ProjectEvent is a server-sent event for project CRUD. JSON keys MUST match the
// frontend ProjectEvent contract in src/composables/useProjects.ts.
type ProjectEvent struct {
	Type      string `json:"type"`
	ProjectID string `json:"projectId"`
	Payload   any    `json:"payload,omitempty"`
}

// ProjectBroadcaster wraps Broadcaster with typed project-event publishing.
type ProjectBroadcaster struct {
	b *Broadcaster
}

// NewProjectBroadcaster creates a ProjectBroadcaster backed by the given Broadcaster.
func NewProjectBroadcaster(b *Broadcaster) *ProjectBroadcaster { return &ProjectBroadcaster{b: b} }

// Broadcast serializes the event and sends it to all SSE subscribers.
// Marshalling errors are dropped — the next reconnect/poll delivers fresh state.
func (p *ProjectBroadcaster) Broadcast(event ProjectEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	p.b.Broadcast(data)
}

// Subscribe returns a channel that receives raw SSE frames for project events.
func (p *ProjectBroadcaster) Subscribe() chan []byte { return p.b.Subscribe() }

// Unsubscribe removes a subscriber channel.
func (p *ProjectBroadcaster) Unsubscribe(ch chan []byte) { p.b.Unsubscribe(ch) }
