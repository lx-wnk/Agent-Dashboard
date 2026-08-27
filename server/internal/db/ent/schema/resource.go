package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Resource is the narrow identity row every managed ARMS resource carries.
// It holds identity, scope, node and lifecycle state — nothing about what the
// resource does. Kind-specific data stays in the kind's own table, which links
// back through resource_id. A single polymorphic table holding process
// addresses, cron expressions and file paths would collapse into dozens of
// nullable columns.
type Resource struct{ ent.Schema }

// Mixin of the Resource.
func (Resource) Mixin() []ent.Mixin {
	return []ent.Mixin{IDTimestampsMixin{}}
}

// Fields of the Resource.
func (Resource) Fields() []ent.Field {
	return []ent.Field{
		// kind: "application" | "routine" | "skill" | "memory_space".
		// A free string rather than an enum: adding a kind must not require a
		// schema migration, the same reasoning that keeps task stages free-form.
		field.String("kind").Immutable(),
		field.String("slug"),
		field.String("name").Default(""),
		// scope_kind: "global" | "project" | "application".
		field.String("scope_kind").Default("global"),
		// scope_ref is "" for global scope. Sentinel, not NULL: SQLite treats
		// two NULLs as distinct, so a nullable column could not carry the unique
		// index below.
		field.String("scope_ref").Default(""),
		// node_id is "local" until the node registry lands. The column exists now
		// because adding it later means a migration through every resource table
		// and every scoped query.
		field.String("node_id").Default("local"),
		// state: "discovered" | "installed" | "enabled" | "disabled" | "orphaned".
		field.String("state").Default("discovered"),
		field.String("version").Default(""),
		// origin: "builtin" | "local" | "remote".
		field.String("origin").Default("local"),
		// origin_ref records where the resource came from — a manifest id, a
		// GitHub source. It is what lets an upstream update avoid silently
		// overwriting a local edit.
		field.String("origin_ref").Default(""),
	}
}

// Indexes of the Resource.
func (Resource) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("kind", "scope_kind", "scope_ref", "slug").Unique(),
		index.Fields("kind", "state"),
	}
}
