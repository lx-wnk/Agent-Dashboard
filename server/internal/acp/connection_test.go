package acp

import (
	"context"
	"io"
	"testing"
	"time"

	sdkacp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

// agentDouble is the peer half of the connection. sdkacp.Agent requires all
// twelve methods below; only Initialize, NewSession and Prompt do anything.
type agentDouble struct {
	conn *sdkacp.AgentSideConnection
}

func (a *agentDouble) Authenticate(ctx context.Context, p sdkacp.AuthenticateRequest) (sdkacp.AuthenticateResponse, error) {
	return sdkacp.AuthenticateResponse{}, nil
}

func (a *agentDouble) Logout(ctx context.Context, p sdkacp.LogoutRequest) (sdkacp.LogoutResponse, error) {
	return sdkacp.LogoutResponse{}, nil
}

func (a *agentDouble) Cancel(ctx context.Context, p sdkacp.CancelNotification) error { return nil }

func (a *agentDouble) CloseSession(ctx context.Context, p sdkacp.CloseSessionRequest) (sdkacp.CloseSessionResponse, error) {
	return sdkacp.CloseSessionResponse{}, nil
}

func (a *agentDouble) DeleteSession(ctx context.Context, p sdkacp.DeleteSessionRequest) (sdkacp.DeleteSessionResponse, error) {
	return sdkacp.DeleteSessionResponse{}, nil
}

func (a *agentDouble) ListSessions(ctx context.Context, p sdkacp.ListSessionsRequest) (sdkacp.ListSessionsResponse, error) {
	return sdkacp.ListSessionsResponse{}, nil
}

func (a *agentDouble) ResumeSession(ctx context.Context, p sdkacp.ResumeSessionRequest) (sdkacp.ResumeSessionResponse, error) {
	return sdkacp.ResumeSessionResponse{}, nil
}

func (a *agentDouble) SetSessionConfigOption(ctx context.Context, p sdkacp.SetSessionConfigOptionRequest) (sdkacp.SetSessionConfigOptionResponse, error) {
	return sdkacp.SetSessionConfigOptionResponse{}, nil
}

func (a *agentDouble) SetSessionMode(ctx context.Context, p sdkacp.SetSessionModeRequest) (sdkacp.SetSessionModeResponse, error) {
	return sdkacp.SetSessionModeResponse{}, nil
}

func (a *agentDouble) Initialize(ctx context.Context, p sdkacp.InitializeRequest) (sdkacp.InitializeResponse, error) {
	return sdkacp.InitializeResponse{ProtocolVersion: sdkacp.ProtocolVersionNumber}, nil
}

func (a *agentDouble) NewSession(ctx context.Context, p sdkacp.NewSessionRequest) (sdkacp.NewSessionResponse, error) {
	return sdkacp.NewSessionResponse{SessionId: "sess-1"}, nil
}

func (a *agentDouble) Prompt(ctx context.Context, p sdkacp.PromptRequest) (sdkacp.PromptResponse, error) {
	_ = a.conn.SessionUpdate(ctx, sdkacp.SessionNotification{
		SessionId: "sess-1",
		Update: sdkacp.SessionUpdate{
			AgentMessageChunk: &sdkacp.SessionUpdateAgentMessageChunk{
				Content: sdkacp.TextBlock("pong"),
			},
		},
	})
	return sdkacp.PromptResponse{StopReason: sdkacp.StopReasonEndTurn}, nil
}

func TestClientReceivesUpdatesOverAConnection(t *testing.T) {
	clientReads, agentWrites := io.Pipe()
	agentReads, clientWrites := io.Pipe()

	events := make(chan Event, 4)
	client := &Client{OnEvent: func(e Event) { events <- e }}
	conn := sdkacp.NewClientSideConnection(client, clientWrites, clientReads)

	agent := &agentDouble{}
	agent.conn = sdkacp.NewAgentSideConnection(agent, agentWrites, agentReads)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := conn.Initialize(ctx, sdkacp.InitializeRequest{ProtocolVersion: sdkacp.ProtocolVersionNumber})
	require.NoError(t, err)

	sess, err := conn.NewSession(ctx, sdkacp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []sdkacp.McpServer{}})
	require.NoError(t, err)
	require.Equal(t, sdkacp.SessionId("sess-1"), sess.SessionId)

	_, err = conn.Prompt(ctx, sdkacp.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []sdkacp.ContentBlock{sdkacp.TextBlock("ping")},
	})
	require.NoError(t, err)

	select {
	case e := <-events:
		require.Equal(t, "agent_message", e.Kind)
		require.Equal(t, "pong", e.Text)
	case <-ctx.Done():
		t.Fatal("no session update arrived")
	}
}
