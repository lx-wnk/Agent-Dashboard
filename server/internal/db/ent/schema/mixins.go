package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"
)

// IDTimestampsMixin carries the id/created_at/updated_at triple that every
// managed entity in this schema currently repeats by hand. New schemas embed it
// so the storage key and the update-default cannot drift between tables.
type IDTimestampsMixin struct{ mixin.Schema }

// Fields of the IDTimestampsMixin.
func (IDTimestampsMixin) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
