package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PromptTemplate stores reusable prompt bodies with {{placeholder}} tokens.
type PromptTemplate struct{ ent.Schema }

func (PromptTemplate) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("name"),
		field.Text("body"),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (PromptTemplate) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name"),
	}
}
