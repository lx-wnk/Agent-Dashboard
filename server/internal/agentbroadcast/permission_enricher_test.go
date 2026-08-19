package agentbroadcast_test

import (
	"context"
	"testing"

	sdk "github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/agentbroadcast"
)

type fakeBridge struct {
	pending    map[string][]sdk.PendingPermission
	atTerminal map[string]bool
}

func (f fakeBridge) PendingForSession(sessionID string) ([]sdk.PendingPermission, bool) {
	return f.pending[sessionID], f.atTerminal[sessionID]
}

func strPtr(s string) *string { return &s }

func TestPermissionBridgeEnricherAttachesHeldRequests(t *testing.T) {
	bridge := fakeBridge{
		pending: map[string][]sdk.PendingPermission{
			"s1": {{ID: "r1", Tool: "Bash", Pattern: strPtr("npm publish")}},
		},
	}
	agents := []sdk.Agent{{SessionID: "s1"}, {SessionID: "s2"}}

	agentbroadcast.NewPermissionBridgeEnricher(bridge)(context.Background(), agents)

	if len(agents[0].PendingPermissions) != 1 || agents[0].PendingPermissions[0].ID != "r1" {
		t.Fatalf("agent s1 pending = %+v, want the held request", agents[0].PendingPermissions)
	}
	if len(agents[1].PendingPermissions) != 0 {
		t.Fatalf("agent s2 picked up %d requests belonging to another session", len(agents[1].PendingPermissions))
	}
}

// An orchestrated agent can hold a pipeline request and a hook-held one at the
// same time; the bridge must add to that list, not replace it.
func TestPermissionBridgeEnricherKeepsPipelineRequests(t *testing.T) {
	bridge := fakeBridge{
		pending: map[string][]sdk.PendingPermission{
			"s1": {{ID: "hook-1", Tool: "Bash"}},
		},
	}
	agents := []sdk.Agent{{
		SessionID:          "s1",
		PendingPermissions: []sdk.PendingPermission{{ID: "pipeline-1", Tool: "Edit"}},
	}}

	agentbroadcast.NewPermissionBridgeEnricher(bridge)(context.Background(), agents)

	if len(agents[0].PendingPermissions) != 2 {
		t.Fatalf("pending = %+v, want both the pipeline and the hook request", agents[0].PendingPermissions)
	}
}

func TestPermissionBridgeEnricherFlagsTheTerminalPrompt(t *testing.T) {
	bridge := fakeBridge{atTerminal: map[string]bool{"s1": true}}
	agents := []sdk.Agent{{SessionID: "s1"}, {SessionID: "s2"}}

	agentbroadcast.NewPermissionBridgeEnricher(bridge)(context.Background(), agents)

	if !agents[0].AwaitingTerminalPermission {
		t.Fatal("s1 is waiting at its terminal but was not flagged")
	}
	if agents[1].AwaitingTerminalPermission {
		t.Fatal("s2 was flagged without a notice of its own")
	}
}

// The no-hooks path must compose away rather than annotate nothing at cost.
func TestPermissionBridgeEnricherIsNilWithoutABridge(t *testing.T) {
	if agentbroadcast.NewPermissionBridgeEnricher(nil) != nil {
		t.Fatal("a nil bridge produced a non-nil enricher")
	}
}

// An agent with no session id has nothing to match on and must be left alone.
func TestPermissionBridgeEnricherSkipsSessionlessAgents(t *testing.T) {
	bridge := fakeBridge{
		pending:    map[string][]sdk.PendingPermission{"": {{ID: "r1"}}},
		atTerminal: map[string]bool{"": true},
	}
	agents := []sdk.Agent{{SessionID: ""}}

	agentbroadcast.NewPermissionBridgeEnricher(bridge)(context.Background(), agents)

	if len(agents[0].PendingPermissions) != 0 || agents[0].AwaitingTerminalPermission {
		t.Fatalf("a sessionless agent was annotated: %+v", agents[0])
	}
}
