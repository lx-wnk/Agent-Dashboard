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
// Unlike SpawnEnforcer, it does not check d.Enforceable for EnforcerServer:
// SpawnEnforcer renders a shared batch of decisions into one allow-list, so
// it must filter out capabilities that aren't its concern. This is the
// complete backstop judging one decision at its point of use, and the other
// two points are each incomplete in their own way — the hook fails open on
// timeout, the spawn point is static and cannot ask — so this one enforces
// every decision handed to it regardless of where else it is enforceable.
func (e ServerEnforcer) Enforce(ctx context.Context, d Decision) error {
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
