package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
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
		field.String("adapter_type").Default("claude"),
		// SQL-level Default needed so SQLite ALTER TABLE ADD COLUMN succeeds
		// on databases with existing rows (ent's Default only fires at Go
		// Create time; JSON columns get no SQL DEFAULT without this).
		// Pass raw "{}" — ent's SQLite dialect already wraps + escapes the
		// value as a SQL string literal, so passing "'{}'" stores literal
		// "''{}''" instead of an empty JSON object.
		field.JSON("adapter_config", map[string]string{}).
			Default(map[string]string{}).
			Annotations(entsql.Default("{}")),
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
