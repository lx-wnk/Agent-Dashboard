package sse

import "encoding/json"

// TaskEvent represents a server-sent event for task state changes.
type TaskEvent struct {
	Type    string `json:"type"`
	TaskID  string `json:"taskId"`
	Payload any    `json:"payload,omitempty"`
}

// TaskBroadcaster wraps Broadcaster with typed task event publishing.
type TaskBroadcaster struct {
	b *Broadcaster
}

// NewTaskBroadcaster creates a TaskBroadcaster backed by the given Broadcaster.
func NewTaskBroadcaster(b *Broadcaster) *TaskBroadcaster {
	return &TaskBroadcaster{b: b}
}

// Broadcast serializes the event and sends it to all SSE subscribers.
// Marshalling errors are silently dropped — the next tick will deliver a fresh snapshot.
func (t *TaskBroadcaster) Broadcast(event TaskEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	t.b.Broadcast(data)
}

// Subscribe returns a channel that receives raw JSON task event frames.
func (t *TaskBroadcaster) Subscribe() chan []byte {
	return t.b.Subscribe()
}

// Unsubscribe removes a subscriber channel.
func (t *TaskBroadcaster) Unsubscribe(ch chan []byte) {
	t.b.Unsubscribe(ch)
}
