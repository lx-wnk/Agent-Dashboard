// server/internal/db/ent/schema/pipeline_config.go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// PipelineConfig holds the schema definition for the PipelineConfig entity.
// Stores key-value pipeline settings (e.g. maxParallelOrchestrators).
type PipelineConfig struct{ ent.Schema }

// Fields of the PipelineConfig.
func (PipelineConfig) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("key"),
		field.String("value"),
	}
}
