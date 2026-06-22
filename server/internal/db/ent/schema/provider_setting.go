package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ProviderSetting persists per-provider enable-state for opt-in agent providers.
type ProviderSetting struct{ ent.Schema }

func (ProviderSetting) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id"),
		field.String("provider_id").Unique(),
		field.Bool("enabled").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ProviderSetting) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider_id"),
	}
}
