package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AgentCostTrend stores per-session cost snapshots for trend analysis.
type AgentCostTrend struct{ ent.Schema }

func (AgentCostTrend) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("session_id"),
		field.String("model"),
		field.Int("input_tokens"),
		field.Int("output_tokens"),
		field.Float("cost_usd"),
		// recorded_at is NOT Immutable so upserts can refresh it to the latest session activity time.
		field.Time("recorded_at").Default(time.Now),
		// cwd is the raw working directory captured from the JSONL session log.
		field.String("cwd").Optional().Default(""),
		// project_path is the resolved grouping key (dashboard folder path, git repo root, or cwd).
		field.String("project_path").Optional().Default(""),
		// project_name is the human-readable display label for the project group.
		field.String("project_name").Optional().Default(""),
		// source_mtime is the file modification time in unix nanoseconds; used to skip unchanged files on rescans.
		field.Int64("source_mtime").Optional().Default(0),
	}
}

func (AgentCostTrend) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id").Unique(),
		index.Fields("recorded_at"),
		index.Fields("project_path"),
	}
}
