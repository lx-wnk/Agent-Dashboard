// server/internal/db/ent/schema/task.go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Task holds the schema definition for the Task entity.
// Phase 2 stub — Phase 3 adds full fields, edges, and indexes.
type Task struct{ ent.Schema }

// Fields of the Task.
func (Task) Fields() []ent.Field {
	return []ent.Field{
		field.String("id"),
		field.String("slug").Unique(),
		field.String("title"),
		field.String("description").Optional(),
		field.String("cwd"),
		field.String("current_stage").Default("concept"),
		field.String("priority").Default("medium"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
