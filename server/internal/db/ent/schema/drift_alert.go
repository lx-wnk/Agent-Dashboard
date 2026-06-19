package schema

import (
	"time"

	"entgo.io/ent"
	entsql "entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type DriftAlert struct{ ent.Schema }

func (DriftAlert) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("spawner_id"),
		field.String("model"),
		field.String("stage"),
		field.String("metric_key"),
		field.String("status").Default("open"),
		field.String("direction"),
		field.Float("baseline_value"),
		field.Float("recent_value"),
		field.Float("delta"),
		field.Float("threshold"),
		field.Int("sample_count"),
		field.Time("detected_at").Default(time.Now),
		field.Time("acknowledged_at").Optional().Nillable(),
	}
}

func (DriftAlert) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"),
		index.Fields("metric_key"),
		// Partial unique: at most one open alert per (spawner_id, model, stage, metric_key).
		index.Fields("spawner_id", "model", "stage", "metric_key").Unique().Annotations(
			entsql.IndexAnnotation{Where: "status = 'open'"},
		),
	}
}
