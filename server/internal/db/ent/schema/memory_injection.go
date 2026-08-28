package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// MemoryInjection records one push of memory entries into a spawn. Without it
// the retrieval heuristic can never be improved, only argued about.
type MemoryInjection struct{ ent.Schema }

// Mixin of the MemoryInjection.
func (MemoryInjection) Mixin() []ent.Mixin {
	return []ent.Mixin{IDTimestampsMixin{}}
}

// Fields of the MemoryInjection.
func (MemoryInjection) Fields() []ent.Field {
	return []ent.Field{
		field.String("stage_run_id").Immutable(),
		field.JSON("entry_ids", []string{}).Default([]string{}),
		field.Int("char_budget"),
		field.Int("chars_used"),
		field.Int("candidate_count"),
	}
}
