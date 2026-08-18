// Package acp adapts the Agent Client Protocol to the dashboard. It implements
// the client half: the agent runs as a separate process and calls into us.
package acp

import (
	"context"
	"errors"
	"log/slog"
	"strings"

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
	ToolKind   string
	ToolName   string
	Locations  []string

	// RawInput is agent-authored, untrusted display data reaching a human and
	// later a UI; never interpolate it into a shell command, a query, or HTML
	// without escaping at the point of use.
	RawInput any
}

// EventKind classifies a normalized session update. Every value Event.Kind can
// carry is named here; a session update this package does not map lands on
// KindOther.
type EventKind string

const (
	KindOther          EventKind = "other"
	KindAgentMessage   EventKind = "agent_message"
	KindAgentThought   EventKind = "agent_thought"
	KindToolCall       EventKind = "tool_call"
	KindToolCallUpdate EventKind = "tool_call_update"
	KindPlan           EventKind = "plan"
	KindMode           EventKind = "mode"
)

// Event is a normalized session update.
type Event struct {
	SessionID  string
	Kind       EventKind
	Text       string
	ToolCallID string
	Status     sdkacp.ToolCallStatus
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

// denyReason separates a gate that considered the request and refused it from
// one that never reached a verdict. Only the former may be answered with
// reject_always, which the agent remembers for the rest of the session.
type denyReason int

const (
	denySubstantive denyReason = iota
	denyTransient
)

func (c *Client) SessionUpdate(ctx context.Context, params sdkacp.SessionNotification) error {
	if c.OnEvent == nil {
		return nil
	}
	e := Event{SessionID: string(params.SessionId), Kind: KindOther}
	switch u := params.Update; {
	case u.AgentMessageChunk != nil:
		e.Kind = KindAgentMessage
		if t := u.AgentMessageChunk.Content.Text; t != nil {
			e.Text = t.Text
		}
	case u.AgentThoughtChunk != nil:
		e.Kind = KindAgentThought
		if t := u.AgentThoughtChunk.Content.Text; t != nil {
			e.Text = t.Text
		}
	case u.ToolCall != nil:
		e.Kind = KindToolCall
		e.ToolCallID = string(u.ToolCall.ToolCallId)
		e.Text = u.ToolCall.Title
		e.Status = u.ToolCall.Status
	case u.ToolCallUpdate != nil:
		e.Kind = KindToolCallUpdate
		e.ToolCallID = string(u.ToolCallUpdate.ToolCallId)
		if s := u.ToolCallUpdate.Status; s != nil {
			e.Status = *s
		}
	case u.Plan != nil:
		e.Kind = KindPlan
	case u.CurrentModeUpdate != nil:
		e.Kind = KindMode
		e.Text = string(u.CurrentModeUpdate.CurrentModeId)
	}
	c.emit(e)
	return nil
}

// emit contains a panicking consumer. OnEvent runs on the SDK's single
// notification goroutine, which recovers nothing, so an unguarded panic there
// takes the whole process down. The event is dropped, the connection lives on.
func (c *Client) emit(e Event) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("acp: OnEvent panicked, dropping event", "kind", e.Kind, "sessionID", e.SessionID, "panic", r)
		}
	}()
	c.OnEvent(e)
}

// ask calls the gate. The named results carry the fail-closed answer before
// the gate is entered, so a panic leaves a transient deny in place: a panicking
// gate must neither authorize the call nor kill the process from the SDK's
// per-request goroutine, and it has reached no verdict worth remembering.
func (c *Client) ask(ctx context.Context, req PermissionRequest) (decision PermissionDecision, reason denyReason) {
	decision, reason = DecisionDeny, denyTransient
	defer func() {
		if r := recover(); r != nil {
			slog.Error("acp: OnPermission panicked, denying", "toolCallID", req.ToolCallID, "sessionID", req.SessionID, "panic", r)
		}
	}()
	d, err := c.OnPermission(ctx, req)
	if err != nil {
		// An unreachable gate must not widen access, and its outage must not
		// outlive itself.
		return DecisionDeny, denyTransient
	}
	return d, denySubstantive
}

func (c *Client) RequestPermission(ctx context.Context, params sdkacp.RequestPermissionRequest) (sdkacp.RequestPermissionResponse, error) {
	decision := DecisionDeny
	// An unwired gate is a wiring fault, not a verdict: treating it as
	// substantive lets the agent remember the refusal and stop asking, which
	// disables approvals for the rest of the session. Transient keeps it
	// recoverable once the gate is supplied.
	reason := denyTransient
	if c.OnPermission == nil {
		slog.Error("acp: no permission gate wired, denying transiently", "toolCallID", string(params.ToolCall.ToolCallId), "sessionID", string(params.SessionId))
	}
	if c.OnPermission != nil {
		req := PermissionRequest{
			SessionID:  string(params.SessionId),
			ToolCallID: string(params.ToolCall.ToolCallId),
		}
		if t := params.ToolCall.Title; t != nil {
			req.Title = *t
		}
		if k := params.ToolCall.Kind; k != nil {
			req.ToolKind = string(*k)
		}
		if n := params.ToolCall.Name; n != nil {
			req.ToolName = *n
		}
		for _, loc := range params.ToolCall.Locations {
			req.Locations = append(req.Locations, loc.Path)
		}
		req.RawInput = params.ToolCall.RawInput
		decision, reason = c.ask(ctx, req)
	}

	wantKinds := []sdkacp.PermissionOptionKind{sdkacp.PermissionOptionKindRejectOnce, sdkacp.PermissionOptionKindRejectAlways}
	switch {
	case decision == DecisionAllow:
		// A missing allow_once must cancel, not fall back to allow_always: the gate
		// approved one call, and allow_always would grant the rest of the session.
		wantKinds = []sdkacp.PermissionOptionKind{sdkacp.PermissionOptionKindAllowOnce}
	case reason == denyTransient:
		// A timeout, an outage or a crashed gate is not a considered rejection;
		// reject_always would turn a momentary failure into a session-wide rule.
		wantKinds = []sdkacp.PermissionOptionKind{sdkacp.PermissionOptionKindRejectOnce}
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
	// No offered option carries the verdict without widening the session, so the
	// answer is Cancelled — the only other member of the outcome union, and the
	// only one that authorizes nothing. It is a fallback, not a real
	// cancellation: no session/cancel was sent, and this client holds no
	// connection handle to send one with. A strict agent reads it as the user
	// cancelling and aborts the entire turn, not just this tool call. Making the
	// outcome honest needs a connection-owning helper that can issue a real
	// session/cancel first (deferred, see F-13/Design Decision 6 of the PR #358
	// review).
	return sdkacp.RequestPermissionResponse{Outcome: sdkacp.RequestPermissionOutcome{
		Cancelled: &sdkacp.RequestPermissionOutcomeCancelled{},
	}}, nil
}

// An option may carry a mode change in its _meta, which grants for the rest of
// the session rather than for this call. This fails closed: the top-level
// _meta keys are matched against "permission" case-insensitively, and more
// than one match is widening (ambiguous). A matched value that isn't a
// map[string]any, or a map with more than one key, is widening. Recognized as
// harmless: no match, an empty map, or a map whose only key is "changes"
// holding an empty slice - everything else, including "changes" holding a
// non-slice or non-empty value, counts as widening. The "changes" lookup
// itself stays case-sensitive; only the top-level "permission" match folds
// case.
func widensSession(o sdkacp.PermissionOption) bool {
	var permRaw any
	matched := false
	for k, v := range o.Meta {
		if !strings.EqualFold(k, "permission") {
			continue
		}
		if matched {
			return true
		}
		permRaw, matched = v, true
	}
	if !matched {
		return false
	}
	perm, ok := permRaw.(map[string]any)
	if !ok {
		return true
	}
	if len(perm) == 0 {
		return false
	}
	if len(perm) != 1 {
		return true
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

// The seven methods below refuse the fs and terminal capabilities
// unconditionally. Whoever advertises this client's capabilities must therefore
// leave InitializeRequest.ClientCapabilities zero — today that is the
// InitializeRequest built in server/internal/llmadapter/llm_acp.go
// (ACPSpawner.Spawn). Advertising a capability here would not fail to compile;
// the agent would call the method mid-turn and get errUnsupported back instead
// of what it was promised. Nothing but this comment ties the two sites
// together; a Capabilities() accessor this package owns is the real fix and is
// deferred to the structural PR that also introduces a connect helper.

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
