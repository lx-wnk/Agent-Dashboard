package capability

import (
	"fmt"
	"time"
)

// Effect is the outcome of a permission decision.
type Effect string

const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
	EffectAsk   Effect = "ask"
)

// Context is one scope a request can be evaluated against — "task t1",
// "global", and so on. Request.Contexts carries the full chain the caller is
// currently in, from most specific to least; Decide does its own ranking, so
// the order of the slice is not itself load-bearing.
type Context struct {
	Kind string
	Ref  string
}

// Request is a single permission question: does this capability, with this
// value, resolve to allow, deny, or ask given the caller's context chain.
type Request struct {
	Capability string
	Value      string
	Contexts   []Context
}

// GrantView is a narrow read-only projection of an ent.Grant row. It exists
// so this package never imports server/internal/db/ent or the repo package,
// keeping Decide pure and database-free.
//
// Capability must equal the Request's Capability for this grant to resolve
// it — a grant for one capability must never resolve a request for another
// just because its Pattern happens to match the requested Value.
//
// LimitCount and LimitWindowSeconds carry the grant's rate limit for the
// enforcer to read. Decide never reads either field — it stays pure and
// evaluates no limit, per the standing rule that rate limits are enforced
// outside the decider (see WithinLimit in enforcer_server.go).
type GrantView struct {
	ID                 string
	Capability         string
	ContextKind        string
	ContextRef         string
	Pattern            string
	Mode               string
	LimitCount         int
	LimitWindowSeconds int
	ExpiresAt          *time.Time
	RevokedAt          *time.Time
}

// CapabilityView is a narrow read-only projection of an ent.Capability row.
type CapabilityView struct {
	Name          string
	Class         string
	EnforceableBy []string
}

// Decision is the resolved answer to a Request.
type Decision struct {
	Effect      Effect
	GrantID     string
	Reason      string
	Enforceable []string
}

// contextRank orders context kinds by specificity; lower is more specific.
// This map is the single source of truth for which context kinds are valid.
// A kind added here without also being accepted by the grant schema's
// validation (or vice versa) makes grants silently inert: Decide drops any
// grant whose ContextKind isn't a key below, on purpose, rather than
// treating an unrecognised kind as least-specific and letting a typo widen
// a grant to global scope.
var contextRank = map[string]int{
	"agent_session": 0,
	"task":          1,
	"routine":       2,
	"application":   3,
	"project":       4,
	"global":        5,
}

// MostSpecific returns the narrowest of contexts — the level Decide would let
// decide if every level had a matching grant. Kinds contextRank does not know
// are skipped, the same ones Decide drops; ok is false when none is known.
//
// Exported so callers that need to name a request's level (a UI label, an
// audit line) rank it against contextRank itself rather than keeping a second
// copy of the ordering.
func MostSpecific(contexts []Context) (best Context, ok bool) {
	bestRank := -1
	for _, c := range contexts {
		rank, known := contextRank[c.Kind]
		if !known || (bestRank != -1 && rank >= bestRank) {
			continue
		}
		best, bestRank = c, rank
	}
	return best, bestRank != -1
}

// modeRank orders grant modes within one context level: deny beats allow
// beats ask; lower wins. This map is the single source of truth for which
// modes are valid. Decide drops any grant whose Mode isn't a key below, on
// purpose, rather than treating an unrecognised mode as rank 0 (deny) and
// letting a typo'd mode silently outrank — or hide behind — an explicit deny.
var modeRank = map[string]int{
	"deny":  0,
	"allow": 1,
	"ask":   2,
}

// Decide resolves a permission request to one decision.
//
// Specificity is resolved before mode: grants are dropped first for being
// revoked, expired, scoped to a different capability, out of scope, or not
// matching the requested value. The remainder is ranked by context
// specificity, and the most specific level with any surviving grant decides
// outright — deny beats allow beats ask only within that level. A broader
// deny never overrules a narrower allow; it never gets a vote once a more
// specific level has any matching grant.
//
// Rate limits are not evaluated here — Decide is pure and holds no counter;
// limit enforcement belongs to the enforcers that track call counts.
func Decide(req Request, grants []GrantView, cap CapabilityView) Decision {
	inScope := make(map[Context]bool, len(req.Contexts))
	for _, c := range req.Contexts {
		inScope[c] = true
	}

	now := time.Now()
	bestRank := -1
	var candidates []*GrantView
	droppedUnknownKind := false

	for i := range grants {
		g := &grants[i]
		if g.RevokedAt != nil {
			continue
		}
		if g.ExpiresAt != nil && g.ExpiresAt.Before(now) {
			continue
		}
		if !Match(g.Pattern, req.Value) {
			continue
		}
		if g.Capability != req.Capability {
			continue
		}
		if !inScope[Context{Kind: g.ContextKind, Ref: g.ContextRef}] {
			continue
		}
		rank, known := contextRank[g.ContextKind]
		if !known {
			droppedUnknownKind = true
			continue
		}
		if _, known := modeRank[g.Mode]; !known {
			// A grant with a mode nobody can parse must not participate in
			// ranking at all, exactly as a grant with an unrecognised
			// context kind (above) does not.
			continue
		}
		switch {
		case bestRank == -1 || rank < bestRank:
			bestRank = rank
			candidates = []*GrantView{g}
		case rank == bestRank:
			candidates = append(candidates, g)
		}
	}

	if len(candidates) == 0 {
		effect := defaultEffect(cap.Class)
		reason := fmt.Sprintf("no matching grant at any context level; class %q defaults to %s", cap.Class, effect)
		if droppedUnknownKind {
			reason += "; a grant was dropped for an unrecognised context kind"
		}
		return Decision{
			Effect:      effect,
			Reason:      reason,
			Enforceable: cap.EnforceableBy,
		}
	}

	winner := candidates[0]
	for _, g := range candidates[1:] {
		if modeRank[g.Mode] < modeRank[winner.Mode] {
			winner = g
		}
	}

	return Decision{
		Effect:      effectForMode(winner.Mode),
		GrantID:     winner.ID,
		Reason:      fmt.Sprintf("%s grant at context %s decided this", winner.Mode, winner.ContextKind),
		Enforceable: cap.EnforceableBy,
	}
}

// effectForMode maps a grant's stored mode to an Effect. An unrecognised
// mode fails safe to ask rather than silently allowing: a grant exists and a
// human authored it, so surfacing it for confirmation is the right failure
// mode. This is deliberately asymmetric with defaultEffect below, where no
// grant exists at all and the safe failure is the opposite direction.
func effectForMode(mode string) Effect {
	switch mode {
	case "allow":
		return EffectAllow
	case "deny":
		return EffectDeny
	default:
		return EffectAsk
	}
}

// defaultEffect is the capability's fallback when no grant at any level
// applies, per spec §4.3 rule 5. "spend" denies unconditionally here — the
// spec's "deny above budget" condition needs a running counter, which the
// Decider does not hold; the budget check itself is the enforcer's job,
// same as rate limits.
//
// An unrecognised class also denies, not asks: falling through to ask would
// turn a typo'd or future class silently into a prompt a human clicks
// through. Deny breaks loudly where the class was introduced instead.
func defaultEffect(class string) Effect {
	switch class {
	case "tool", "reach", "resource":
		return EffectAsk
	case "spend":
		return EffectDeny
	default:
		return EffectDeny
	}
}
