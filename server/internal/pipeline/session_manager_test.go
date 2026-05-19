package pipeline_test

import (
	"os"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/stretchr/testify/require"
)

func TestIsPidAlive_Self(t *testing.T) {
	require.True(t, pipeline.IsPidAlive(os.Getpid()))
}

func TestIsPidAlive_Zero(t *testing.T) {
	require.False(t, pipeline.IsPidAlive(0))
}

func TestIsPidAlive_Negative(t *testing.T) {
	require.False(t, pipeline.IsPidAlive(-1))
}

func TestIsPidAlive_DeadPID(t *testing.T) {
	require.False(t, pipeline.IsPidAlive(999999))
}

func TestBuildSessionName(t *testing.T) {
	got := pipeline.BuildSessionName("fix-login", "implementation", 3)
	require.Equal(t, "fix-login-implementation-iter-3", got)
}

func TestDecideRecovery_LivePID(t *testing.T) {
	pid := os.Getpid()
	sr := &ent.StageRun{Pid: &pid}
	decision := pipeline.DecideRecovery(sr)
	require.Equal(t, "alive", decision.Kind)
}

func TestDecideRecovery_DeadPID_WithSession(t *testing.T) {
	pid := 999999
	sessionID := "sess-abc"
	sr := &ent.StageRun{Pid: &pid, SessionID: &sessionID}
	decision := pipeline.DecideRecovery(sr)
	require.Equal(t, "resume", decision.Kind)
}

func TestDecideRecovery_DeadPID_NoSession(t *testing.T) {
	pid := 999999
	sr := &ent.StageRun{Pid: &pid}
	decision := pipeline.DecideRecovery(sr)
	require.Equal(t, "restart", decision.Kind)
}

func TestDecideRecovery_NilPID(t *testing.T) {
	sr := &ent.StageRun{}
	decision := pipeline.DecideRecovery(sr)
	require.Equal(t, "restart", decision.Kind)
}
