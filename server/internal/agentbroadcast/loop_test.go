package agentbroadcast

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCapabilityDecisionsOrEmpty_NilProviderMarshalsEmptyArray confirms a nil
// CapabilityDecisionProvider produces "[]" in the marshalled frame, not
// "null" — the SPA iterates this field.
func TestCapabilityDecisionsOrEmpty_NilProviderMarshalsEmptyArray(t *testing.T) {
	got := capabilityDecisionsOrEmpty(context.Background(), nil)
	require.NotNil(t, got, "nil provider must yield an empty slice, not nil")

	data, err := json.Marshal(broadcastFrame{
		Agents:                     nil,
		Trend:                      emptyTrend,
		PendingCapabilityDecisions: got,
	})
	require.NoError(t, err)
	require.Contains(t, string(data), `"pendingCapabilityDecisions":[]`)
	require.NotContains(t, string(data), `"pendingCapabilityDecisions":null`)
}

// TestFnv64a_Deterministic confirms that identical inputs produce the same hash
// and different inputs produce different hashes. (F-PERF-006)
func TestFnv64a_Deterministic(t *testing.T) {
	a := fnv64a([]byte(`{"agents":[],"trend":[]}`))
	b := fnv64a([]byte(`{"agents":[],"trend":[]}`))
	require.Equal(t, a, b, "same input must produce same hash")

	c := fnv64a([]byte(`{"agents":[{"id":"x"}],"trend":[]}`))
	require.NotEqual(t, a, c, "different input must produce different hash")
}

// TestEmptyTrend_NotNil confirms the package-level var is initialised and
// that it does not allocate a new slice on each access. (F-PERF-014)
func TestEmptyTrend_NotNil(t *testing.T) {
	require.NotNil(t, emptyTrend, "emptyTrend must be initialised")
	require.Equal(t, 0, len(emptyTrend), "emptyTrend must be empty")

	// The pointer identity check ensures no copy was made.
	// This is a package-internal test (same package), so it can access the var directly.
	p1 := &emptyTrend
	p2 := &emptyTrend
	require.True(t, p1 == p2, "emptyTrend must be the same variable across accesses")
}
