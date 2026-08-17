package acp

import (
	"context"
	"errors"
	"testing"

	sdkacp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

type fakeModeSetter struct {
	got []sdkacp.SessionModeId
	err error
}

func (f *fakeModeSetter) SetSessionMode(ctx context.Context, p sdkacp.SetSessionModeRequest) (sdkacp.SetSessionModeResponse, error) {
	f.got = append(f.got, p.ModeId)
	return sdkacp.SetSessionModeResponse{}, f.err
}

func modeState(current string, available ...string) *sdkacp.SessionModeState {
	st := &sdkacp.SessionModeState{CurrentModeId: sdkacp.SessionModeId(current)}
	for _, a := range available {
		st.AvailableModes = append(st.AvailableModes, sdkacp.SessionMode{Id: sdkacp.SessionModeId(a), Name: a})
	}
	return st
}

func TestEnsureModeSetsTheRequestedMode(t *testing.T) {
	f := &fakeModeSetter{}
	err := EnsureMode(context.Background(), f, "sess-1", modeState("auto", "auto", "default"), ModeGated)
	require.NoError(t, err)
	require.Equal(t, []sdkacp.SessionModeId{ModeGated}, f.got)
}

func TestEnsureModeFailsWhenTheAgentCannotOfferIt(t *testing.T) {
	f := &fakeModeSetter{}
	err := EnsureMode(context.Background(), f, "sess-1", modeState("auto", "auto", "bypassPermissions"), ModeGated)
	require.Error(t, err)
	require.Empty(t, f.got, "must not ask for a mode the agent did not advertise")
}

func TestEnsureModeFailsWhenTheAgentRejectsTheChange(t *testing.T) {
	f := &fakeModeSetter{err: errors.New("refused")}
	err := EnsureMode(context.Background(), f, "sess-1", modeState("auto", "auto", "default"), ModeGated)
	require.Error(t, err)
}

func TestEnsureModeFailsOnMissingModeState(t *testing.T) {
	f := &fakeModeSetter{}
	err := EnsureMode(context.Background(), f, "sess-1", nil, ModeGated)
	require.Error(t, err, "an agent that advertises no modes cannot be gated")
	require.Empty(t, f.got)
}

func TestEnsureModeStillSetsWhenCurrentModeAlreadyMatches(t *testing.T) {
	f := &fakeModeSetter{}
	err := EnsureMode(context.Background(), f, "sess-1", modeState("default", "auto", "default"), ModeGated)
	require.NoError(t, err)
	require.Equal(t, []sdkacp.SessionModeId{ModeGated}, f.got, "the advertised current mode is a claim, not a guarantee")
}
