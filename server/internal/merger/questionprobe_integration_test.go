package merger_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
	"github.com/lx-wnk/agent-dashboard/server/internal/testsupport/fakespawn"
)

// TestGetAgents_PendingQuestion_InjectableAgentUsesProbe verifies an
// injectable agent's PendingQuestion is populated from a configured probe.
func TestGetAgents_PendingQuestion_InjectableAgentUsesProbe(t *testing.T) {
	fs := fakespawn.New(t)
	ag := fs.Spawn(fakespawn.SpawnOpts{LiveInjectable: true})

	want := &sdk.DetectedQuestion{
		Header:   "Pick one",
		Question: "Pick one?",
		Options: []sdk.DetectedOption{
			{Index: 1, Label: "Red"},
			{Index: 2, Label: "Green"},
		},
		TypeSomethingIndex: 3,
		ChatAboutIndex:     4,
	}

	m := merger.New(
		merger.WithScanFn(fs.ScanFn()),
		merger.WithQuestionProbe(func(pid int) *sdk.DetectedQuestion {
			if pid == ag.PID {
				return want
			}
			return nil
		}),
	)

	agents, err := m.GetAgents(context.Background(), merger.GetAgentsOpts{})
	require.NoError(t, err)
	require.Len(t, agents, 1)
	require.True(t, agents[0].LiveInjectable, "fixture must be injectable")
	assert.Equal(t, want, agents[0].PendingQuestion)
}

// TestGetAgents_PendingQuestion_NilProbeLeavesFieldNil verifies that with no
// probe configured, PendingQuestion stays nil even for an injectable agent.
func TestGetAgents_PendingQuestion_NilProbeLeavesFieldNil(t *testing.T) {
	fs := fakespawn.New(t)
	fs.Spawn(fakespawn.SpawnOpts{LiveInjectable: true})

	m := merger.New(merger.WithScanFn(fs.ScanFn()))

	agents, err := m.GetAgents(context.Background(), merger.GetAgentsOpts{})
	require.NoError(t, err)
	require.Len(t, agents, 1)
	require.True(t, agents[0].LiveInjectable)
	assert.Nil(t, agents[0].PendingQuestion)
}

// TestGetAgents_PendingQuestion_NonInjectableAgentProbeNotCalled verifies the
// probe is only invoked for injectable agents — a non-injectable agent (e.g.
// a plain terminal session with a channel-bridge file but no tmuxPane/pty)
// must never trigger a probe call, and its PendingQuestion stays nil.
func TestGetAgents_PendingQuestion_NonInjectableAgentProbeNotCalled(t *testing.T) {
	fs := fakespawn.New(t)
	ag := fs.Spawn(fakespawn.SpawnOpts{})

	called := false
	m := merger.New(
		merger.WithScanFn(fs.ScanFn()),
		merger.WithQuestionProbe(func(pid int) *sdk.DetectedQuestion {
			called = true
			return &sdk.DetectedQuestion{Header: "should not be called"}
		}),
	)

	agents, err := m.GetAgents(context.Background(), merger.GetAgentsOpts{})
	require.NoError(t, err)
	require.Len(t, agents, 1)
	require.Equal(t, ag.PID, agents[0].PID)
	assert.False(t, agents[0].LiveInjectable, "fixture must not be injectable")
	assert.False(t, called, "probe must not be called for a non-injectable agent")
	assert.Nil(t, agents[0].PendingQuestion)
}
