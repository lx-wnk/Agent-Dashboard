package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AuditEvent records security-relevant actions for incident forensics.
// Unlike AuditLog (task-scoped), AuditEvent is free-standing and covers
// cross-cutting security events: spawn, permission grants/revokes, key lifecycle.
type AuditEvent struct{ ent.Schema }

func (AuditEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.Time("ts").Default(time.Now).Immutable(),
		field.String("user_id").Optional().Nillable().Immutable(),
		// action is one of: spawn, spawn_rejected, permission_grant,
		// permission_revoke, key_create, key_delete.
		field.String("action").Immutable(),
		// target identifies the subject of the action (task ID, key ID, etc.).
		field.String("target").Immutable(),
		field.JSON("metadata", map[string]any{}).Optional().Immutable(),
	}
}

func (AuditEvent) Edges() []ent.Edge { return nil }

func (AuditEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("ts"),
		index.Fields("user_id", "action"),
	}
}
