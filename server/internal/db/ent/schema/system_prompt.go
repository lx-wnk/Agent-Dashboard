package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SystemPrompt holds user-defined prompt overrides injected into pipeline stages.
type SystemPrompt struct{ ent.Schema }

func (SystemPrompt) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		// scope: "global" (all tasks) or "task" (specific task only, not yet wired)
		field.String("scope").Default("global"),
		// stage: nil means all stages; non-nil targets a specific stage name.
		field.String("stage").Optional().Nillable(),
		field.Text("content"),
		// priority: higher number wins when multiple prompts match.
		field.Int("priority").Default(0),
		field.String("created_by").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (SystemPrompt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("scope", "stage"),
		index.Fields("priority"),
	}
}
