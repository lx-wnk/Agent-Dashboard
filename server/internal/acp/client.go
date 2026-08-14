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

// Client implements sdkacp.Client. Both callbacks may be nil.
type Client struct {
	OnEvent      func(Event)
	OnPermission func(context.Context, PermissionRequest) (PermissionDecision, error)
}

var _ sdkacp.Client = (*Client)(nil)

var errUnsupported = errors.New("acp: capability not offered by this client")

func (c *Client) SessionUpdate(ctx context.Context, params sdkacp.SessionNotification) error {
	return nil
}

func (c *Client) RequestPermission(ctx context.Context, params sdkacp.RequestPermissionRequest) (sdkacp.RequestPermissionResponse, error) {
	return sdkacp.RequestPermissionResponse{}, errUnsupported
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
