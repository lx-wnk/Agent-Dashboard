package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Plugin persists discovered/installed/active state per plugin (Shopware-style:
// installed_at nullable + active bool). State is derived: no installed_at =
// discovered; set + active=false = inactive; active=true = active.
type Plugin struct{ ent.Schema }

func (Plugin) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(), // the manifest id
		field.String("name").Default(""),
		field.String("version").Default(""),
		field.Time("installed_at").Optional().Nillable(),
		field.Bool("active").Default(false),
		field.String("path").Default(""),
		field.String("manifest_hash").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Plugin) Indexes() []ent.Index {
	return []ent.Index{index.Fields("active")}
}
