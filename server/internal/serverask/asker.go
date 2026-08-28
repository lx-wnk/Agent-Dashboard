// Package serverask implements capability.Asker for the in-process
// ServerEnforcer, backed by askgate.Store. It lives outside askgate, which is
// deliberately generic and must not depend on the policy package, and
// outside capability, which must not depend on a concrete hold-and-answer
// mechanism — an asker necessarily imports both, so it gets its own package.
package serverask

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/askgate"
	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
)

// askHoldTimeout is how long Ask waits for a human decision before failing
// closed. It is bounded by MCP and browser client request timeouts, not by
// Claude Code's own hook timeout — unlike the hook's permissionHoldTimeout,
// which is sized against that instead. The two happen to match today; that
// is coincidence, not a shared constant, and either may change independently.
const askHoldTimeout = 25 * time.Second

// contextSpecificity mirrors capability's own (unexported) context ranking,
// duplicated here for display only: it only picks which context label a
// pending ask shows a human, never a decision, so a stale copy costs a wrong
// label, not a wrong enforcement outcome.
var contextSpecificity = map[string]int{
	"agent_session": 0,
	"task":          1,
	"routine":       2,
	"application":   3,
	"project":       4,
	"global":        5,
}

// Pending is one server-point decision waiting for a human, as shown in the UI.
type Pending struct {
	Capability  string
	Value       string
	Context     string // the most specific context of the request, rendered
	Reason      string
	RequestedAt time.Time
}

// Asker answers capability.EffectAsk decisions raised by ServerEnforcer by
// holding the call open until a human resolves it through Resolve.
type Asker struct {
	store *askgate.Store[Pending]
}

var _ capability.Asker = (*Asker)(nil)

// New returns an Asker whose Ask waits up to askHoldTimeout for a decision.
// onChange, which may be nil, is called whenever the pending set changes.
func New(onChange func()) *Asker {
	return &Asker{store: askgate.New[Pending](askHoldTimeout, onChange)}
}

// Ask registers req and d as a pending decision and blocks for a human answer.
//
// Unlike HookEnforcer, which fails OPEN on no decision because its fallback
// is Claude Code's own terminal prompt, this point has no such fallback —
// ServerEnforcer is the complete backstop — so no decision here means
// refuse, never proceed.
func (a *Asker) Ask(ctx context.Context, req capability.Request, d capability.Decision) (bool, error) {
	id := uuid.New().String()
	meta := Pending{
		Capability:  req.Capability,
		Value:       req.Value,
		Context:     mostSpecificContext(req.Contexts),
		Reason:      d.Reason,
		RequestedAt: time.Now(),
	}
	decision, ok := a.store.Ask(ctx, id, meta)
	if !ok {
		return false, nil
	}
	return decision == "allow", nil
}

// mostSpecificContext renders the context a human approving this ask should
// see. req.Contexts' order is documented as not load-bearing, so this ranks
// by contextSpecificity instead of assuming index 0 is the most specific.
func mostSpecificContext(contexts []capability.Context) string {
	if len(contexts) == 0 {
		return ""
	}
	best := contexts[0]
	bestRank, ok := contextSpecificity[best.Kind]
	if !ok {
		bestRank = len(contextSpecificity)
	}
	for _, c := range contexts[1:] {
		rank, ok := contextSpecificity[c.Kind]
		if !ok {
			rank = len(contextSpecificity)
		}
		if rank < bestRank {
			best, bestRank = c, rank
		}
	}
	if best.Ref == "" {
		return best.Kind
	}
	return best.Kind + " " + best.Ref
}

// Pending returns a snapshot of every ask currently waiting for a decision.
func (a *Asker) Pending() []askgate.Entry[Pending] {
	return a.store.List()
}

// ErrInvalidDecision means Resolve was given a decision string that is
// neither "allow" nor "deny".
var ErrInvalidDecision = errors.New("serverask: decision must be \"allow\" or \"deny\"")

// Resolve delivers a decision to the ask named by id.
func (a *Asker) Resolve(id, decision string) error {
	if decision != "allow" && decision != "deny" {
		return ErrInvalidDecision
	}
	return a.store.Resolve(id, func(Pending) (string, error) {
		return decision, nil
	})
}
