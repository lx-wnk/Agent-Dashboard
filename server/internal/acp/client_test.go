package acp

import (
	"context"
	"testing"

	sdkacp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

func TestClientSatisfiesSDKInterface(t *testing.T) {
	var c any = &Client{}
	_, ok := c.(sdkacp.Client)
	require.True(t, ok, "Client must implement sdkacp.Client")
}

func TestSessionUpdateEmitsAgentMessage(t *testing.T) {
	var got []Event
	c := &Client{OnEvent: func(e Event) { got = append(got, e) }}

	err := c.SessionUpdate(context.Background(), sdkacp.SessionNotification{
		SessionId: "sess-1",
		Update: sdkacp.SessionUpdate{
			AgentMessageChunk: &sdkacp.SessionUpdateAgentMessageChunk{
				Content: sdkacp.TextBlock("hello"),
			},
		},
	})

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "sess-1", got[0].SessionID)
	require.Equal(t, "agent_message", got[0].Kind)
	require.Equal(t, "hello", got[0].Text)
}

func TestSessionUpdateEmitsToolCall(t *testing.T) {
	var got []Event
	c := &Client{OnEvent: func(e Event) { got = append(got, e) }}

	err := c.SessionUpdate(context.Background(), sdkacp.SessionNotification{
		SessionId: "sess-1",
		Update: sdkacp.SessionUpdate{
			ToolCall: &sdkacp.SessionUpdateToolCall{
				ToolCallId: "tc-1",
				Title:      "Write file",
			},
		},
	})

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "tool_call", got[0].Kind)
	require.Equal(t, "tc-1", got[0].ToolCallID)
	require.Equal(t, "Write file", got[0].Text)
}

func TestSessionUpdateWithoutCallbackDoesNotPanic(t *testing.T) {
	c := &Client{}
	err := c.SessionUpdate(context.Background(), sdkacp.SessionNotification{
		SessionId: "sess-1",
		Update: sdkacp.SessionUpdate{
			AgentMessageChunk: &sdkacp.SessionUpdateAgentMessageChunk{
				Content: sdkacp.TextBlock("hello"),
			},
		},
	})
	require.NoError(t, err)
}
