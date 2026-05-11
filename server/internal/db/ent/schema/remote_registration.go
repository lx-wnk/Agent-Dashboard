package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RemoteRegistration stores registered remote dashboard instances.
type RemoteRegistration struct{ ent.Schema }

func (RemoteRegistration) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("user_id"),
		field.String("url"),
		field.String("name").Optional().Nillable(),
		field.String("bearer_key").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (RemoteRegistration) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
	}
}
