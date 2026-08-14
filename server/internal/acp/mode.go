package acp

import (
	"context"
	"fmt"

	sdkacp "github.com/coder/acp-go-sdk"
)

// ModeGated is the only session mode under which the agent asks before acting.
// The other modes either approve silently or refuse without asking.
const ModeGated sdkacp.SessionModeId = "default"

// ModeSetter is the part of the ACP connection EnsureMode needs.
type ModeSetter interface {
	SetSessionMode(ctx context.Context, params sdkacp.SetSessionModeRequest) (sdkacp.SetSessionModeResponse, error)
}

// EnsureMode pins a session to want. A session inherits its mode from the
// operator's settings, so an unpinned session may approve every tool call
// without ever reaching the gate.
func EnsureMode(ctx context.Context, s ModeSetter, sessionID sdkacp.SessionId, state *sdkacp.SessionModeState, want sdkacp.SessionModeId) error {
	if state == nil {
		return fmt.Errorf("acp: session %s advertises no modes, cannot pin to %q", sessionID, want)
	}
	offered := false
	for _, m := range state.AvailableModes {
		if m.Id == want {
			offered = true
			break
		}
	}
	if !offered {
		return fmt.Errorf("acp: session %s does not offer mode %q", sessionID, want)
	}
	if _, err := s.SetSessionMode(ctx, sdkacp.SetSessionModeRequest{SessionId: sessionID, ModeId: want}); err != nil {
		return fmt.Errorf("acp: pinning session %s to %q: %w", sessionID, want, err)
	}
	return nil
}
