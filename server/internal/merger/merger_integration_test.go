// Package merger_test provides integration tests for the merger package.
package merger_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdk "github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
)

// TestGetAgents_DoesNotPanic verifies that GetAgents does not panic when called
// in an environment with no running Claude processes.
// It may return an error (e.g. scanner not available) or an empty slice — both are valid.
func TestGetAgents_DoesNotPanic(t *testing.T) {
	agents, err := merger.GetAgents(context.Background(), merger.GetAgentsOpts{})
	if err != nil {
		// Scanner may fail in CI — acceptable.
		t.Logf("GetAgents returned error (acceptable in CI): %v", err)
		return
	}
	// If no error, the result must be a valid (possibly empty) slice of the correct type.
	require.NotNil(t, agents)
	assert.IsType(t, []sdk.Agent{}, agents)
}
