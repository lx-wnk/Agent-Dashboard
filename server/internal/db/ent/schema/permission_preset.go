package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PermissionPreset stores reusable tool-permission presets per user and project.
type PermissionPreset struct{ ent.Schema }

func (PermissionPreset) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("user_id").Optional().Nillable(),
		field.String("project_cwd"),
		field.String("tool"),
		field.String("pattern").Optional().Nillable(),
	}
}

func (PermissionPreset) Indexes() []ent.Index {
	return []ent.Index{
		// NOTE: SQLite treats two NULL values as distinct, so this UNIQUE index
		// will not prevent duplicate (NULL user_id, cwd, tool, NULL pattern) rows.
		// INSERT OR IGNORE in the repo still works correctly for non-null patterns.
		index.Fields("user_id", "project_cwd", "tool", "pattern").Unique(),
	}
}
