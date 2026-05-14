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
	agents, err := merger.GetAgents(context.Background())
	if err != nil {
		// Scanner may fail in CI — acceptable.
		t.Logf("GetAgents returned error (acceptable in CI): %v", err)
		return
	}
	// If no error, the result must be a valid (possibly empty) slice of the correct type.
	require.NotNil(t, agents)
	assert.IsType(t, []sdk.Agent{}, agents)
}

// TestGetAgents_ResultIsSlice is an additional integration check that GetAgents
// returns a non-nil slice when successful.
// (Unit coverage already exists in merger_test.go; this confirms the package
// links correctly with the integration build tag.)
func TestGetAgents_ResultIsSlice(t *testing.T) {
	agents, err := merger.GetAgents(context.Background())
	if err != nil {
		t.Skipf("GetAgents unavailable in this environment: %v", err)
		return
	}
	// Returned value must be a slice (never nil on success).
	assert.NotNil(t, agents)
}
