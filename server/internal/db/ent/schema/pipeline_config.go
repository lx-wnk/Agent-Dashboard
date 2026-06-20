// server/internal/db/ent/schema/pipeline_config.go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PipelineConfig holds the schema definition for the PipelineConfig entity.
// Stores key-value pipeline settings (e.g. maxParallelOrchestrators).
// project_id="" means global scope; a non-empty project_id scopes the row
// to that project and takes precedence over the global row.
type PipelineConfig struct{ ent.Schema }

// Fields of the PipelineConfig.
func (PipelineConfig) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("key").Immutable(),
		// "" = global scope; any non-empty value = project scope.
		// Using a sentinel instead of NULL so the unique index (project_id, key) fires correctly on SQLite.
		field.String("project_id").Default(""),
		field.String("value"),
	}
}

// Indexes of the PipelineConfig.
func (PipelineConfig) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "key").Unique(),
	}
}
