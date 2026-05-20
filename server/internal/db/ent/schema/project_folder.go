package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ProjectFolder holds the schema definition for the ProjectFolder entity.
type ProjectFolder struct{ ent.Schema }

// Fields of the ProjectFolder.
func (ProjectFolder) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("path").MaxLen(4096),
		field.String("label").Optional().Nillable(),
		field.Bool("is_default").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

// Edges of the ProjectFolder.
func (ProjectFolder) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).Ref("folders").Unique().Required(),
	}
}

// Indexes of the ProjectFolder.
func (ProjectFolder) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("project").Fields("path").Unique(),
	}
}
