package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ApplicationSecret struct{ ent.Schema }

func (ApplicationSecret) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("resource_id").Immutable(),
		field.String("env_name").Immutable(),
		field.String("ciphertext").Sensitive(),
		field.String("nonce").Sensitive(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ApplicationSecret) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("resource_id", "env_name").Unique(),
	}
}
