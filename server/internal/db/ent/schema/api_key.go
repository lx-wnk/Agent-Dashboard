// server/internal/db/ent/schema/api_key.go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ApiKey holds the schema definition for the ApiKey entity.
type ApiKey struct{ ent.Schema }

// Fields of the ApiKey.
func (ApiKey) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id"),
		field.String("name").Unique(),
		field.String("key_hash").Unique().Sensitive(),
		field.JSON("scopes", []string{}).Default([]string{}),
		field.Bool("active").Default(true),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("last_used_at").Optional().Nillable(),
	}
}

// Indexes of the ApiKey.
func (ApiKey) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("key_hash"),
		index.Fields("active"),
	}
}
