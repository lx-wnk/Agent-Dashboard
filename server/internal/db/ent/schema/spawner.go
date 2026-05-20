package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Spawner holds the schema definition for the Spawner entity.
type Spawner struct{ ent.Schema }

// Fields of the Spawner.
func (Spawner) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("name"),
		field.String("slug").Unique(),
		field.String("command"),
		field.JSON("args", []string{}).Default([]string{}),
		field.JSON("env", map[string]string{}).Default(map[string]string{}),
		field.String("model_override").Optional().Nillable(),
		field.String("description").Optional().Nillable(),
		field.Bool("built_in").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes of the Spawner.
func (Spawner) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("slug"),
	}
}
