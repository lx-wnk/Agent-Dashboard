package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Scratchpad is shared key/value coordination state, keyed by a free-string namespace.
type Scratchpad struct{ ent.Schema }

func (Scratchpad) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("namespace"),
		field.String("key"),
		field.Text("value"),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
		field.String("updated_by_task_id"),
	}
}

func (Scratchpad) Indexes() []ent.Index {
	return []ent.Index{index.Fields("namespace", "key").Unique()}
}
