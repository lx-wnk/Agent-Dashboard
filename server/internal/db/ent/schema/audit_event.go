package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AuditEvent records security-relevant actions for incident forensics.
// It is the single canonical audit table. AuditLog (task-scoped) has been
// deprecated and its rows are migrated into this table with task_id populated.
type AuditEvent struct{ ent.Schema }

func (AuditEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.Time("ts").Default(time.Now).Immutable(),
		field.String("user_id").Optional().Nillable().Immutable(),
		// action is one of: spawn, spawn_rejected, permission_grant,
		// permission_revoke, key_create, key_delete, task_done, task_cancelled,
		// retry_requested, stage transitions, and any pipeline-internal actions.
		field.String("action").Immutable(),
		// target identifies the subject of the action (task ID, key ID, etc.).
		field.String("target").Immutable(),
		field.JSON("metadata", map[string]any{}).Optional().Immutable(),
		// task_id is an optional foreign key linking the event to a specific task.
		// Populated for all pipeline events; nil for cross-cutting events (key lifecycle, etc.).
		field.String("task_id").Optional().Nillable().Immutable(),
	}
}

func (AuditEvent) Edges() []ent.Edge { return nil }

func (AuditEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("ts"),
		index.Fields("user_id", "action"),
		index.Fields("task_id"),
	}
}
