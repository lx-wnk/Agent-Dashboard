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
		field.Time("recorded_at").Default(time.Now).Immutable(),
	}
}

func (AgentCostTrend) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id"),
		index.Fields("recorded_at"),
	}
}
