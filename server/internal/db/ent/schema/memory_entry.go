package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// MemoryEntry is one conclusion held by a memory space: what the system knows,
// since when, where it came from, and whether it still holds. A markdown file
// can express none of that.
type MemoryEntry struct{ ent.Schema }

// Mixin of the MemoryEntry.
func (MemoryEntry) Mixin() []ent.Mixin {
	return []ent.Mixin{IDTimestampsMixin{}}
}

// Fields of the MemoryEntry.
func (MemoryEntry) Fields() []ent.Field {
	return []ent.Field{
		// space_id is the resource row (kind = memory_space) this entry belongs
		// to. A plain id, not an edge: resource is the shared identity table for
		// every ARMS resource kind, and an edge back to it would tie that generic
		// table to one specific kind's reverse reference.
		field.String("space_id").Immutable(),
		// summary is what gets pushed into a spawn's prompt; content is what gets
		// pulled on demand. Kept as its own column rather than derived so the
		// push budget is spent on a summary the author wrote, not on however many
		// characters of content happened to come first.
		field.String("summary"),
		field.Text("content"),
		// kind: "fact" | "preference" | "lesson" | "entity" | "pointer".
		field.String("kind"),
		// source_kind: "agent" | "user" | "application" | "import".
		field.String("source_kind"),
		// source_ref: stage-run id, application id, file path — whatever
		// identifies the origin. Nillable because not every source has one.
		field.String("source_ref").Optional().Nillable(),
		// confidence is a probability; bounds are enforced here rather than left
		// to callers, matching this branch's fail-closed posture on invalid input.
		field.Float("confidence").Min(0).Max(1),
		field.Time("valid_from").Default(time.Now),
		// valid_until: nil means open-ended.
		field.Time("valid_until").Optional().Nillable(),
		// superseded_by: the id of the entry that replaced this one. Set on the
		// old entry rather than mutating it, so the chain stays an audit trail.
		field.String("superseded_by").Optional().Nillable(),
		// user_id: row scoping, mirroring tasks. Nillable means unscoped.
		field.String("user_id").Optional().Nillable(),
	}
}

// Indexes of the MemoryEntry.
func (MemoryEntry) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("space_id", "valid_until"),
		index.Fields("space_id", "kind"),
		index.Fields("superseded_by"),
	}
}
