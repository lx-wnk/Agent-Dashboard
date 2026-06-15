package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type EvalMetricSnapshot struct{ ent.Schema }

func (EvalMetricSnapshot) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("spawner_id"),
		field.String("model"),
		field.String("stage"),
		field.String("metric_key"),
		field.Float("value"),
		field.Int("sample_count"),
		field.Time("window_start"),
		field.Time("window_end"),
		field.Time("recorded_at").Default(time.Now),
	}
}

func (EvalMetricSnapshot) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("recorded_at"),
		index.Fields("metric_key"),
		index.Fields("spawner_id", "model", "stage"),
	}
}
