package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Checkpoint is a per-turn worktree snapshot captured into a hidden git ref
// refs/checkpoints/<task_id>/<seq>. It records the snapshot's tree+commit SHA
// so the worktree can be restored to that exact state.
type Checkpoint struct{ ent.Schema }

func (Checkpoint) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("task_id").Immutable(),
		field.String("stage_run_id").Optional().Nillable(),
		field.Int("seq"),
		field.String("commit_sha"),
		field.String("tree_sha"),
		field.Int("files_changed").Default(0),
		field.Bool("pre_revert").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Checkpoint) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("task_id"),
		index.Fields("task_id", "seq").Unique(),
	}
}
