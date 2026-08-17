package acp

import (
	"context"
	"fmt"

	sdkacp "github.com/coder/acp-go-sdk"
)

// ModeGated is the session mode under which the agent asks before acting,
// measured against Claude Code as of 2026-08-14: its other modes either
// approve silently or refuse without asking. ACP itself defines no mode
// semantics, so this id is one agent's convention, not a protocol guarantee —
// another ACP agent may name its asking mode differently or offer none, which
// is what EnsureMode's want parameter is for.
const ModeGated sdkacp.SessionModeId = "default"

// ModeSetter is the part of the ACP connection EnsureMode needs.
type ModeSetter interface {
	SetSessionMode(ctx context.Context, params sdkacp.SetSessionModeRequest) (sdkacp.SetSessionModeResponse, error)
}

// EnsureMode asks the agent to pin a session to want. A nil return means the
// request was accepted, not that the session is gated: want was advertised in
// state and SetSessionMode returned no error. SetSessionModeResponse carries
// no confirmation field, and nothing stops the agent from leaving that mode
// afterwards, so the guarantee ends the moment this call returns.
//
// The only signal that reports a session's actual mode is the agent's
// current_mode_update notification, which Client.SessionUpdate surfaces as
// Event{Kind: "mode", Text: <mode id>}. It has no consumer today, so drift is
// unguarded: a session that ends up in a silently-approving mode never calls
// RequestPermission at all, and the fail-closed gate never runs — tool calls
// then execute unreviewed instead of being denied. A watcher on that event,
// abandoning or re-pinning the session when the reported mode is not want, is
// what would upgrade this contract to "verified gated".
//
// An error means the session is definitely not gated; the caller must abandon
// it rather than proceed.
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
