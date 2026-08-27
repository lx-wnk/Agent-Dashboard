package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
)

// Capability is a named permission coarser than a tool name, so an action
// like sending mail can be expressed at all — the existing allow-list can
// only name Claude Code tools.
type Capability struct{ ent.Schema }

// Mixin of the Capability.
func (Capability) Mixin() []ent.Mixin {
	return []ent.Mixin{IDTimestampsMixin{}}
}

// Fields of the Capability.
func (Capability) Fields() []ent.Field {
	return []ent.Field{
		// name: "mail.send", "Bash", "memory.write" — the capability's identity.
		// Unique() is a column-level constraint, not a named Indexes() entry
		// like resource's — no pre-seeded index in client.go is needed or
		// meaningful here; do not add one "for consistency" with resource.
		field.String("name").Unique(),
		// class: "tool" | "reach" | "resource" | "spend".
		field.String("class"),
		// enforceable_by: subset of "server", "spawn", "hook" — where this
		// capability can actually be enforced. Enforcement is not uniform: the
		// PreToolUse hook fails open by design, so a capability that only holds
		// for orchestrated agents must say so rather than implying a guarantee
		// the system cannot keep.
		// SQL-level Default needed so SQLite ALTER TABLE ADD COLUMN succeeds on
		// databases with existing rows (ent's Default only fires at Go Create
		// time; JSON columns get no SQL DEFAULT without this). Pass raw "[]" —
		// ent's SQLite dialect already wraps + escapes the value as a SQL
		// string literal, so passing "'[]'" stores the literal "''[]''"
		// instead of an empty JSON array.
		field.JSON("enforceable_by", []string{}).
			Default([]string{}).
			Annotations(entsql.Default("[]")),
		field.Bool("requires_pattern").Default(false),
		field.Bool("reversible").Default(false),
		field.String("description").Default(""),
	}
}
