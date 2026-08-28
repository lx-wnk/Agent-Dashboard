package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// GrantUsage records one successful use of a rate-limited grant. The
// enforcer counts rows in a sliding window to decide whether a grant's
// limit_count/limit_window_seconds is exhausted — Decide itself never reads
// those columns or this table.
type GrantUsage struct{ ent.Schema }

// Mixin of the GrantUsage.
func (GrantUsage) Mixin() []ent.Mixin {
	return []ent.Mixin{IDTimestampsMixin{}}
}

// Fields of the GrantUsage.
func (GrantUsage) Fields() []ent.Field {
	return []ent.Field{
		field.String("grant_id").Immutable(),
		// used_at is distinct from the mixin's created_at so a test can seed
		// rows at explicit historical times without depending on insert order.
		field.Time("used_at").Default(time.Now).Immutable(),
	}
}

// Indexes of the GrantUsage.
func (GrantUsage) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("grant_id", "used_at"),
	}
}
