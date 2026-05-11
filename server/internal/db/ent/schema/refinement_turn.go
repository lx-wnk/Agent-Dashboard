package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RefinementTurn holds conversation turns for the refinement chat feature.
// task_id is a plain string field — no ent edge back to Task to avoid circular dependency.
type RefinementTurn struct{ ent.Schema }

func (RefinementTurn) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("task_id"),
		field.Enum("role").Values("user", "assistant"),
		field.String("content"),
		field.String("phase").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (RefinementTurn) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("task_id", "created_at"),
	}
}
