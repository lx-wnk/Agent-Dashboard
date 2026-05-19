package merger_test

import (
	"testing"
	"time"

	sdk "github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
	"github.com/stretchr/testify/require"
)

// TestCalculateStatus_ExactBoundaries verifies the exact threshold boundaries.
// active < 30s, waiting < 5min (300s), idle >= 5min.
func TestCalculateStatus_ExactBoundaries(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name         string
		lastActivity time.Time
		want         sdk.AgentStatus
	}{
		// Active threshold: < 30s
		{"30s ago is waiting not active", now.Add(-30 * time.Second), sdk.AgentStatusWaiting},
		{"31s ago is waiting", now.Add(-31 * time.Second), sdk.AgentStatusWaiting},
		{"1s ago is active", now.Add(-1 * time.Second), sdk.AgentStatusActive},
		{"just now is active", now, sdk.AgentStatusActive},
		// Waiting threshold: < 5min (300s)
		{"5min ago is idle", now.Add(-5 * time.Minute), sdk.AgentStatusIdle},
		{"299s ago is waiting", now.Add(-299 * time.Second), sdk.AgentStatusWaiting},
		{"300s ago is idle", now.Add(-300 * time.Second), sdk.AgentStatusIdle},
		// Far past should be idle.
		{"1 hour ago is idle", now.Add(-1 * time.Hour), sdk.AgentStatusIdle},
		{"1 day ago is idle", now.Add(-24 * time.Hour), sdk.AgentStatusIdle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := merger.CalculateStatus(tt.lastActivity)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestCalculateStatus_FutureTimestamp verifies that a future timestamp
// (e.g. clock skew) is treated as active.
func TestCalculateStatus_FutureTimestamp(t *testing.T) {
	future := time.Now().Add(5 * time.Second)
	got := merger.CalculateStatus(future)
	require.Equal(t, sdk.AgentStatusActive, got)
}
