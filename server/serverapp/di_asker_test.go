package serverapp

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/memory"
	"github.com/lx-wnk/agent-dashboard/server/internal/serverask"
)

// A nil *serverask.Asker assigned straight into an interface field yields a
// NON-nil interface holding a nil pointer. memory.Gate.Authorize branches on
// `Asker == nil`, so the plain assignment would send an ask decision to a nil
// pointer and panic instead of denying — in auth mode none, where nobody can
// answer in the first place. Both helpers exist only to prevent that, so both
// are pinned here.
func TestAskerHelpersReturnGenuinelyNilInterfaces(t *testing.T) {
	if got := askerArgFor(nil); got != nil {
		t.Errorf("askerArgFor(nil) = %#v, want a nil interface — memory.Gate.Authorize tests Asker == nil", got)
	}
	if got := capabilityAskerFor(nil); got != nil {
		t.Errorf("capabilityAskerFor(nil) = %#v, want a nil interface — the router tests deps.CapabilityAsker != nil", got)
	}

	present := serverask.New(nil)
	if askerArgFor(present) == nil {
		t.Error("askerArgFor(asker) = nil, want the asker — a wired asker must reach the Gate")
	}
	if capabilityAskerFor(present) == nil {
		t.Error("capabilityAskerFor(asker) = nil, want the asker — a wired asker must receive the rescan")
	}
}

// TestGateBuiltLikeBypassAuthDeniesRatherThanPanics exercises the combination
// the helper above protects: the Gate DI builds when no asker is wired must
// answer an ask decision, not crash on one.
func TestGateBuiltLikeBypassAuthDeniesRatherThanPanics(t *testing.T) {
	gate := memory.Gate{Asker: askerArgFor(nil)}
	if gate.Asker != nil {
		t.Fatal("Gate.Asker is non-nil with no asker wired — Authorize would dereference it")
	}
}
