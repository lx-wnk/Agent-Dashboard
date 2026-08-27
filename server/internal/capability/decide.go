package capability

import "time"

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
type GrantView struct {
	ID          string
	ContextKind string
	ContextRef  string
	Pattern     string
	Mode        string
	ExpiresAt   *time.Time
	RevokedAt   *time.Time
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
var contextRank = map[string]int{
	"agent_session": 0,
	"task":          1,
	"routine":       2,
	"application":   3,
	"project":       4,
	"global":        5,
}

// modeRank orders grant modes within one context level: deny beats allow
// beats ask; lower wins.
var modeRank = map[string]int{
	"deny":  0,
	"allow": 1,
	"ask":   2,
}

// Decide resolves a permission request to one decision.
//
// Specificity is resolved before mode: grants are dropped first for being
// revoked, expired, out of scope, or not matching the requested value. The
// remainder is ranked by context specificity, and the most specific level
// with any surviving grant decides outright — deny beats allow beats ask
// only within that level. A broader deny never overrules a narrower allow;
// it never gets a vote once a more specific level has any matching grant.
//
// Rate limits are not evaluated here — Decide is pure and holds no counter;
// limit enforcement belongs to the enforcers that track call counts.
func Decide(req Request, grants []GrantView, cap CapabilityView) Decision {
	inScope := make(map[string]string, len(req.Contexts))
	for _, c := range req.Contexts {
		inScope[c.Kind] = c.Ref
	}

	now := time.Now()
	bestRank := -1
	var candidates []*GrantView

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
		ref, inRequestChain := inScope[g.ContextKind]
		if !inRequestChain || ref != g.ContextRef {
			continue
		}
		rank, known := contextRank[g.ContextKind]
		if !known {
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
		return Decision{
			Effect:      defaultEffect(cap.Class),
			Reason:      "no matching grant at any context level; falling back to the capability's default",
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
		Reason:      "resolved from the most specific matching grant",
		Enforceable: cap.EnforceableBy,
	}
}

// effectForMode maps a grant's stored mode to an Effect. An unrecognised
// mode fails safe to ask rather than silently allowing.
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
// applies: ask for tool and reach (and any other class), deny for spend —
// spend is the one class where the safe default is refusal, not a prompt.
func defaultEffect(class string) Effect {
	if class == "spend" {
		return EffectDeny
	}
	return EffectAsk
}
