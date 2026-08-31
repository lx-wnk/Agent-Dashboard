package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type TaskPermission struct{ ent.Schema }

func (TaskPermission) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("task_id").Immutable(),
		field.String("tool"),
		field.String("pattern").Optional().Nillable(),
		field.Bool("granted").Default(false),
		field.Bool("manual_override").Default(false),
		field.String("decided_by").Optional().Nillable(),
		field.Time("requested_at").Default(time.Now),
		field.Time("decided_at").Optional().Nillable(),
		field.Time("expires_at").Optional().Nillable(),
	}
}

func (TaskPermission) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("task", Task.Type).Ref("permissions").Field("task_id").Unique().Required().Immutable(),
	}
}

func (TaskPermission) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("task_id"),
	}
}
