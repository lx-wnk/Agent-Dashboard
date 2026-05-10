package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type TaskDependency struct{ ent.Schema }

func (TaskDependency) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("task_id").Immutable(),
		field.String("depends_on_id").Immutable(),
		field.String("required_stage").Default("done"),
		field.String("on_cancel_action").Default("on_hold"),
	}
}

func (TaskDependency) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("task", Task.Type).Ref("dependencies").Field("task_id").Unique().Required().Immutable(),
		edge.From("depends_on", Task.Type).Ref("dependents").Field("depends_on_id").Unique().Required().Immutable(),
	}
}

func (TaskDependency) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("task_id"),
		index.Fields("depends_on_id"),
		index.Fields("task_id", "depends_on_id").Unique(),
	}
}
