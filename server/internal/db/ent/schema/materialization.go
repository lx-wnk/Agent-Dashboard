package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Materialization records what one resource produced at one target. It is the
// ownership proof: cmd/serve/hooks.go:252-263 established for settings.json
// that a marker inside a file is not proof of ownership because a copy carries
// it too — the path is. This table holds those paths.
type Materialization struct{ ent.Schema }

// Mixin of the Materialization.
func (Materialization) Mixin() []ent.Mixin {
	return []ent.Mixin{IDTimestampsMixin{}}
}

// Fields of the Materialization.
func (Materialization) Fields() []ent.Field {
	return []ent.Field{
		field.String("resource_id").Immutable(),
		// target_key identifies the target within the node × config dir ×
		// provider cross product: "<provider>|<layer>|<root>". Stable across
		// runs by construction — a changed key orphans this row, and the next
		// run would then report the file it wrote itself as foreign.
		field.String("target_key").Immutable(),
		field.String("path"),
		// content_hash is the SHA-256 of the bytes last written here. The empty
		// string means this node has never written these bytes — which is what
		// a foreign row records, and what keeps a foreign file foreign instead
		// of reading as a conflict against a hash that was never taken.
		field.String("content_hash").Default(""),
		// outcome is the last classification: created | unchanged | repaired |
		// conflict | foreign. Stored so a report can say when a conflict was
		// first seen without re-deriving it from the filesystem.
		field.String("outcome").Default(""),
	}
}

// Indexes of the Materialization.
func (Materialization) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("resource_id", "target_key").Unique(),
		index.Fields("outcome"),
	}
}
