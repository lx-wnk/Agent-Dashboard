package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type PermissionRequest struct{ ent.Schema }

func (PermissionRequest) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("stage_run_id").Immutable(),
		field.String("tool"),
		field.String("pattern").Optional().Nillable(),
		field.String("reason").Optional().Nillable(),
		field.String("outcome").Optional().Nillable(),
		field.Time("requested_at").Default(time.Now),
		field.Time("resolved_at").Optional().Nillable(),
	}
}

func (PermissionRequest) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("stage_run", StageRun.Type).Ref("permission_requests").Field("stage_run_id").Unique().Required().Immutable(),
	}
}

func (PermissionRequest) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("stage_run_id"),
		index.Fields("outcome"),
	}
}
