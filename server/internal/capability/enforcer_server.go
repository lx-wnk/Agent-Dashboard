package capability

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrDenied means the action was refused. Callers surface it to the agent
	// as a denial it can act on, never as a silent no-op.
	ErrDenied = errors.New("capability denied")
	// ErrAskRequired means a decision needed a human and no asker was wired.
	ErrAskRequired = errors.New("capability requires approval but no asker is configured")
)

// Asker routes an ask-effect decision to whoever answers it.
type Asker interface {
	Ask(ctx context.Context, d Decision) (bool, error)
}

// ServerEnforcer intercepts in-process application calls. It is the only
// enforcement point with complete coverage: nothing routes around it, and
// unlike the hook it cannot be bypassed by a timeout.
type ServerEnforcer struct {
	Asker Asker
}

// Point identifies this enforcement point.
func (ServerEnforcer) Point() string { return EnforcerServer }

// Enforce returns nil when the action may proceed.
//
// It first checks g's rate limit: an EffectAllow decision whose grant g is
// exhausted (see WithinLimit) is downgraded to ask here, before the switch
// below runs — never to a silent deny, which would be indistinguishable
// from having no grant at all, and never left as a silent allow, which
// would let the limit go unenforced. The reason names the limit so the user
// can tell which cap they hit (spec §6). A grant with no limit (the zero
// value GrantView{}, LimitCount 0) passes this check trivially, so callers
// with nothing to rate-limit may pass GrantView{} and usedInWindow 0. Only
// EffectAllow is limit-checked; deny and ask already carry their own reason
// and are unaffected by usage.
//
// Unlike SpawnEnforcer, it does not check d.Enforceable for EnforcerServer:
// SpawnEnforcer renders a shared batch of decisions into one allow-list, so
// it must filter out capabilities that aren't its concern. This is the
// complete backstop judging one decision at its point of use, and the other
// two points are each incomplete in their own way — the hook fails open on
// timeout, the spawn point is static and cannot ask — so this one enforces
// every decision handed to it regardless of where else it is enforceable.
func (e ServerEnforcer) Enforce(ctx context.Context, d Decision, g GrantView, usedInWindow int) error {
	if d.Effect == EffectAllow && !WithinLimit(g, usedInWindow) {
		d = Decision{
			Effect:      EffectAsk,
			GrantID:     d.GrantID,
			Reason:      fmt.Sprintf("rate limit exceeded: grant %s allows %d use(s) per %ds, %d used", g.ID, g.LimitCount, g.LimitWindowSeconds, usedInWindow),
			Enforceable: d.Enforceable,
		}
	}
	switch d.Effect {
	case EffectAllow:
		return nil
	case EffectDeny:
		return fmt.Errorf("%w: %s", ErrDenied, d.Reason)
	case EffectAsk:
		if e.Asker == nil {
			// A missing asker is a wiring bug. Failing closed is the only safe
			// reading: an enforcer that allows what it cannot adjudicate is
			// worse than one that is absent.
			return fmt.Errorf("%w: %s", ErrAskRequired, d.Reason)
		}
		ok, err := e.Asker.Ask(ctx, d)
		if err != nil {
			return fmt.Errorf("capability ask: %w", err)
		}
		if !ok {
			return fmt.Errorf("%w: refused when asked", ErrDenied)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown effect %q", ErrDenied, d.Effect)
	}
}

// WithinLimit reports whether a grant carrying LimitCount/LimitWindowSeconds
// still has room for one more use, given how many uses already fall inside
// the window.
//
// LimitCount 0 is the documented "unlimited" sentinel. A negative LimitCount
// is not a valid limit and is never treated as unlimited — it fails closed
// to exhausted, matching the other unrecognised-value decisions on this
// branch (unknown capability class denies, unknown grant mode asks, unknown
// effect denies): no invalid value resolves to allow.
//
// usedInWindow equal to LimitCount is exhausted — a limit of three permits
// three calls, not four.
//
// Pure: no clock, no database. The caller (an enforcer) supplies the count,
// keeping capability.Decide itself free of any limit evaluation.
func WithinLimit(g GrantView, usedInWindow int) bool {
	switch {
	case g.LimitCount < 0:
		return false
	case g.LimitCount == 0:
		return true
	default:
		return usedInWindow < g.LimitCount
	}
}
