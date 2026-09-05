package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Skill is the content half of a skill resource: what the materializer turns
// into a SKILL.md. The identity half — slug, scope, node, state, origin —
// lives on the resource row this one points at, which is why there is no slug
// column here to disagree with it.
type Skill struct{ ent.Schema }

// Mixin of the Skill.
func (Skill) Mixin() []ent.Mixin {
	return []ent.Mixin{IDTimestampsMixin{}}
}

// Fields of the Skill.
func (Skill) Fields() []ent.Field {
	return []ent.Field{
		// resource_id is the resource row (kind = skill) this content belongs
		// to. A plain id, not an edge, for the reason memory_entry.space_id
		// gives: resource is the shared identity table for every ARMS kind, and
		// an edge back would tie that generic table to one kind's reverse
		// reference.
		field.String("resource_id").Immutable().Unique(),
		// description becomes the SKILL.md frontmatter `description:`, which is
		// what a runtime shows in its skill menu.
		field.String("description").Default(""),
		// body is everything below the frontmatter. The frontmatter itself is
		// rendered, never stored: it carries the ownership marker, and a stored
		// marker could be edited into something the materializer does not own.
		field.Text("body").Default(""),
	}
}

// Indexes of the Skill.
func (Skill) Indexes() []ent.Index {
	return []ent.Index{index.Fields("resource_id").Unique()}
}
