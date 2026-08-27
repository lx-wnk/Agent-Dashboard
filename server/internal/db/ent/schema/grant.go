package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Grant binds a capability to a context — this project, this routine, this
// agent session — and carries the three things the permission tables cannot
// express: an expiry, a rate limit, and a negative mode a narrower context
// can use to overrule a broader allow.
type Grant struct{ ent.Schema }

// Mixin of the Grant.
func (Grant) Mixin() []ent.Mixin {
	return []ent.Mixin{IDTimestampsMixin{}}
}

// Fields of the Grant.
func (Grant) Fields() []ent.Field {
	return []ent.Field{
		field.String("capability_name"),
		// context_kind: "global" | "project" | "task" | "routine" |
		// "application" | "agent_session".
		field.String("context_kind"),
		// context_ref is "" for the global context — the sentinel, not NULL.
		// SQLite treats two NULLs as distinct, so a nullable column could not
		// carry the compound lookup index below; repo.Scope relies on the
		// same convention for resources.
		field.String("context_ref").Default(""),
		// pattern: validated by capability.ParsePattern before a grant is
		// stored. Empty string is the documented wildcard.
		field.String("pattern").Default(""),
		// mode: "allow" | "deny" | "ask".
		field.String("mode"),
		// limit_count: 0 means unlimited. Min(0) rejects a negative value at
		// write time — capability.WithinLimit also fails a negative value
		// closed (exhausted, never unlimited) as a second, independent
		// guard for any row that predates this constraint.
		field.Int("limit_count").Default(0).Min(0),
		field.Int("limit_window_seconds").Default(0),
		field.Time("expires_at").Optional().Nillable(),
		// granted_by is required, not nillable. Its predecessor,
		// task_permission.decided_by, is nillable and is consequently
		// written by nothing, which is why the system cannot currently
		// answer who allowed a given action.
		field.String("granted_by"),
		field.Time("granted_at").Default(time.Now).Immutable(),
		field.Time("revoked_at").Optional().Nillable(),
		// revoked_by mirrors granted_by: a revocation is equally a security
		// decision and needs an actor, or "who revoked this" becomes
		// unanswerable the same way "who allowed this" was. Unlike
		// granted_by it is not required-non-empty — an un-revoked row
		// legitimately has none — so "" means "not revoked", not "revoked
		// by nobody". The non-empty constraint lives in Revoke.
		field.String("revoked_by").Default(""),
		field.String("reason").Default(""),
		// node_id is "local" until the node registry lands.
		field.String("node_id").Default("local"),
	}
}

// Indexes of the Grant.
func (Grant) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("capability_name", "context_kind", "context_ref"),
		index.Fields("revoked_at"),
	}
}
