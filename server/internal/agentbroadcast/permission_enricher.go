package agentbroadcast

import (
	"context"

	sdk "github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
)

// PermissionBridgeReader is the read side of the hook permission bridge. It is
// an interface rather than the concrete type so this package does not import
// api/hooks, which would point the layer arrow the wrong way (the API layer
// already depends on the broadcast side).
type PermissionBridgeReader interface {
	// PendingForSession returns the requests currently answerable from the
	// dashboard for one session, and whether that session's own terminal is
	// showing a prompt nobody answered here.
	PendingForSession(sessionID string) ([]sdk.PendingPermission, bool)
}

// NewPermissionBridgeEnricher annotates each agent with the permission prompts
// the bridge knows about.
//
// Unlike the pipeline enricher next to it, this is positive evidence rather than
// inference: the request came from the session itself, through its PreToolUse
// hook, carrying the tool and the exact argument it is asking about. Nothing is
// reconstructed from the transcript, where a tool call blocked on approval and
// one merely still running leave the same trace.
//
// A nil reader returns a nil enricher, which merger.ChainEnrichers skips.
func NewPermissionBridgeEnricher(bridge PermissionBridgeReader) merger.Enricher {
	if bridge == nil {
		return nil
	}
	return func(_ context.Context, agents []sdk.Agent) {
		for i := range agents {
			sid := agents[i].SessionID
			if sid == "" {
				continue
			}
			pending, atTerminal := bridge.PendingForSession(sid)
			if len(pending) > 0 {
				// Held calls only ever belong to a session that is blocked on
				// this decision, so they append rather than replace: an
				// orchestrated agent can carry a pipeline request as well.
				agents[i].PendingPermissions = append(agents[i].PendingPermissions, pending...)
			}
			agents[i].AwaitingTerminalPermission = atTerminal
		}
	}
}
