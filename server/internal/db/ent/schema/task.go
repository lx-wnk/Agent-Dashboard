package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Task struct{ ent.Schema }

func (Task) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("slug").Unique(),
		field.String("title").MaxLen(200),
		field.String("description").Optional().Nillable(),
		field.String("cwd").MaxLen(4096),
		field.String("worktree_path").Optional().Nillable(),
		field.String("source_branch").Optional().Nillable(),
		field.String("target_branch").Optional().Nillable(),
		field.String("current_stage").Default("concept"),
		field.String("priority").Default("medium"),
		field.String("user_id").Optional().Nillable(),
		field.String("parent_task_id").Optional().Nillable(),
		field.Int("max_iterations").Default(20),
		field.Int("token_budget").Optional().Nillable(),
		field.Int("cost_budget_cents").Optional().Nillable(),
		field.Int("stage_timeout_seconds").Default(1800),
		field.Bool("silver_bullet").Default(false),
		field.JSON("metadata", map[string]any{}).Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Task) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("stage_runs", StageRun.Type),
		edge.To("permissions", TaskPermission.Type),
		edge.To("audit_logs", AuditLog.Type),
		edge.To("dependencies", TaskDependency.Type),
		edge.To("dependents", TaskDependency.Type),
	}
}

func (Task) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("current_stage"),
		index.Fields("parent_task_id"),
		index.Fields("silver_bullet", "priority", "created_at"),
	}
}
