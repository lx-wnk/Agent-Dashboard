package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// AppSetting is a generic key/value store for DB-backed, non-bootstrap
// configuration. The value is an opaque string interpreted by the Go setting
// registry (internal/settings).
type AppSetting struct{ ent.Schema }

func (AppSetting) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("key").Unique(),
		field.String("value"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
