package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type AuditLog struct{ ent.Schema }

func (AuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("task_id").Immutable(),
		field.String("actor"),
		field.String("action"),
		field.JSON("details", map[string]any{}).Optional(),
		field.Time("timestamp").Default(time.Now).Immutable(),
	}
}

func (AuditLog) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("task", Task.Type).Ref("audit_logs").Field("task_id").Unique().Required().Immutable(),
	}
}

func (AuditLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("task_id"),
		index.Fields("timestamp"),
	}
}
