package taskcontrol_test

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/taskcontrol"
	"github.com/stretchr/testify/require"
)

func TestFromFields_BasicMapping(t *testing.T) {
	s := taskcontrol.FromFields("concept", "running", "", 0, false)
	require.Equal(t, "concept", s.Stage)
	require.Equal(t, "running", s.RunStatus)
	require.Equal(t, 0, s.PendingPerms)
	require.False(t, s.NeedsUser)
}

func TestFromFields_NeedsUserPropagated(t *testing.T) {
	s := taskcontrol.FromFields("implementation", "awaiting_user", "", 3, true)
	require.Equal(t, 3, s.PendingPerms)
	require.True(t, s.NeedsUser)
}

func TestFromFields_NoRunStatus(t *testing.T) {
	// Task with no stage run yet — RunStatus should be empty string.
	s := taskcontrol.FromFields("concept", "", "", 0, false)
	require.Empty(t, s.RunStatus)
}
