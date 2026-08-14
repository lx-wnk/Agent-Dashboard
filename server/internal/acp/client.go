// Package acp adapts the Agent Client Protocol to the dashboard. It implements
// the client half: the agent runs as a separate process and calls into us.
package acp

import (
	"context"
	"errors"

	sdkacp "github.com/coder/acp-go-sdk"
)

// PermissionDecision is the caller's answer to a permission request.
type PermissionDecision int

const (
	// DecisionDeny is the zero value so an unset or failed decision blocks.
	DecisionDeny PermissionDecision = iota
	DecisionAllow
)

// PermissionRequest describes a tool call the agent wants to run.
type PermissionRequest struct {
	SessionID  string
	ToolCallID string
	Title      string
}

// Event is a normalized session update.
type Event struct {
	SessionID  string
	Kind       string
	Text       string
	ToolCallID string
	Status     string
}

// Client implements sdkacp.Client.
//
// OnEvent runs on the connection's single ordered notification goroutine and
// must not block; once 1024 notifications queue up the SDK closes the
// connection.
//
// OnPermission runs on a per-request goroutine, so it may block waiting for a
// human, and it should honour the passed context, which is cancelled on
// disconnect.
//
// Both fields must be set before the client is handed to the connection;
// assigning them afterwards races with those goroutines.
type Client struct {
	OnEvent      func(Event)
	OnPermission func(context.Context, PermissionRequest) (PermissionDecision, error)
}

var _ sdkacp.Client = (*Client)(nil)

var errUnsupported = errors.New("acp: capability not offered by this client")

func (c *Client) SessionUpdate(ctx context.Context, params sdkacp.SessionNotification) error {
	if c.OnEvent == nil {
		return nil
	}
	e := Event{SessionID: string(params.SessionId), Kind: "other"}
	switch u := params.Update; {
	case u.AgentMessageChunk != nil:
		e.Kind = "agent_message"
		if t := u.AgentMessageChunk.Content.Text; t != nil {
			e.Text = t.Text
		}
	case u.AgentThoughtChunk != nil:
		e.Kind = "agent_thought"
		if t := u.AgentThoughtChunk.Content.Text; t != nil {
			e.Text = t.Text
		}
	case u.ToolCall != nil:
		e.Kind = "tool_call"
		e.ToolCallID = string(u.ToolCall.ToolCallId)
		e.Text = u.ToolCall.Title
		e.Status = string(u.ToolCall.Status)
	case u.ToolCallUpdate != nil:
		e.Kind = "tool_call_update"
		e.ToolCallID = string(u.ToolCallUpdate.ToolCallId)
		if s := u.ToolCallUpdate.Status; s != nil {
			e.Status = string(*s)
		}
	case u.Plan != nil:
		e.Kind = "plan"
	case u.CurrentModeUpdate != nil:
		e.Kind = "mode"
		e.Text = string(u.CurrentModeUpdate.CurrentModeId)
	}
	c.OnEvent(e)
	return nil
}

func (c *Client) RequestPermission(ctx context.Context, params sdkacp.RequestPermissionRequest) (sdkacp.RequestPermissionResponse, error) {
	decision := DecisionDeny
	if c.OnPermission != nil {
		req := PermissionRequest{
			SessionID:  string(params.SessionId),
			ToolCallID: string(params.ToolCall.ToolCallId),
		}
		if t := params.ToolCall.Title; t != nil {
			req.Title = *t
		}
		// An unreachable gate must not widen access.
		if d, err := c.OnPermission(ctx, req); err == nil {
			decision = d
		}
	}

	wantKinds := []sdkacp.PermissionOptionKind{sdkacp.PermissionOptionKindRejectOnce, sdkacp.PermissionOptionKindRejectAlways}
	if decision == DecisionAllow {
		// A missing allow_once must cancel, not fall back to allow_always: the gate
		// approved one call, and allow_always would grant the rest of the session.
		wantKinds = []sdkacp.PermissionOptionKind{sdkacp.PermissionOptionKindAllowOnce}
	}
	for _, want := range wantKinds {
		for _, o := range params.Options {
			if o.Kind == want && !widensSession(o) {
				return sdkacp.RequestPermissionResponse{Outcome: sdkacp.RequestPermissionOutcome{
					Selected: &sdkacp.RequestPermissionOutcomeSelected{OptionId: o.OptionId},
				}}, nil
			}
		}
	}
	return sdkacp.RequestPermissionResponse{Outcome: sdkacp.RequestPermissionOutcome{
		Cancelled: &sdkacp.RequestPermissionOutcomeCancelled{},
	}}, nil
}

// An option may carry a mode change in its _meta, which grants for the rest of
// the session rather than for this call. This fails closed: only a small set
// of shapes is positively recognized as harmless (no "permission" key, an
// empty "permission" map, or a "changes" key holding an empty slice) -
// everything else, including a "permission" map that omits "changes"
// entirely, counts as widening.
func widensSession(o sdkacp.PermissionOption) bool {
	permRaw, ok := o.Meta["permission"]
	if !ok {
		return false
	}
	perm, ok := permRaw.(map[string]any)
	if !ok {
		return true
	}
	if len(perm) == 0 {
		return false
	}
	changesRaw, ok := perm["changes"]
	if !ok {
		return true
	}
	changes, ok := changesRaw.([]any)
	if !ok {
		return true
	}
	return len(changes) > 0
}

func (c *Client) ReadTextFile(ctx context.Context, params sdkacp.ReadTextFileRequest) (sdkacp.ReadTextFileResponse, error) {
	return sdkacp.ReadTextFileResponse{}, errUnsupported
}

func (c *Client) WriteTextFile(ctx context.Context, params sdkacp.WriteTextFileRequest) (sdkacp.WriteTextFileResponse, error) {
	return sdkacp.WriteTextFileResponse{}, errUnsupported
}

func (c *Client) CreateTerminal(ctx context.Context, params sdkacp.CreateTerminalRequest) (sdkacp.CreateTerminalResponse, error) {
	return sdkacp.CreateTerminalResponse{}, errUnsupported
}

func (c *Client) KillTerminal(ctx context.Context, params sdkacp.KillTerminalRequest) (sdkacp.KillTerminalResponse, error) {
	return sdkacp.KillTerminalResponse{}, errUnsupported
}

func (c *Client) TerminalOutput(ctx context.Context, params sdkacp.TerminalOutputRequest) (sdkacp.TerminalOutputResponse, error) {
	return sdkacp.TerminalOutputResponse{}, errUnsupported
}

func (c *Client) ReleaseTerminal(ctx context.Context, params sdkacp.ReleaseTerminalRequest) (sdkacp.ReleaseTerminalResponse, error) {
	return sdkacp.ReleaseTerminalResponse{}, errUnsupported
}

func (c *Client) WaitForTerminalExit(ctx context.Context, params sdkacp.WaitForTerminalExitRequest) (sdkacp.WaitForTerminalExitResponse, error) {
	return sdkacp.WaitForTerminalExitResponse{}, errUnsupported
}
