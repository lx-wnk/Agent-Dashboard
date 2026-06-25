package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// CoordLock is a lease-based mutual-exclusion lock keyed by a free-string namespace.
type CoordLock struct{ ent.Schema }

func (CoordLock) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("namespace"),
		field.String("key"),
		field.String("owner_task_id"),
		field.Time("acquired_at").Default(time.Now),
		field.Time("expires_at"),
	}
}

func (CoordLock) Indexes() []ent.Index {
	return []ent.Index{index.Fields("namespace", "key").Unique()}
}
