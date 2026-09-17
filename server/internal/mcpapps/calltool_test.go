package mcpapps_test

import (
	"context"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/mcpapps"
)

// fakeAccountServer answers like the published mail server does: a single text
// content whose body is JSON. A second tool fails, and a third returns the same
// payload as structured content.
func fakeAccountServer(t *testing.T) mcp.Transport {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "fake-accounts", Version: "0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "list_accounts"},
		func(ctx context.Context, req *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{
				&mcp.TextContent{Text: "{\n  \"accounts\": [{\"name\": \"work@example.com\"}]\n}"},
			}}, nil, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "explodes"},
		func(ctx context.Context, req *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
			return nil, nil, errors.New("mailbox unreachable")
		})
	serverT, clientT := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = server.Run(ctx, serverT) }()
	return clientT
}

func TestCallTool_ReturnsTheTextContentBody(t *testing.T) {
	raw, err := mcpapps.CallTool(context.Background(), fakeAccountServer(t), "list_accounts", nil)
	require.NoError(t, err)
	require.JSONEq(t, `{"accounts":[{"name":"work@example.com"}]}`, string(raw))
}

func TestCallTool_UnknownToolNamesIt(t *testing.T) {
	_, err := mcpapps.CallTool(context.Background(), fakeAccountServer(t), "no_such_tool", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no_such_tool")
}

func TestCallTool_ServerErrorIsReported(t *testing.T) {
	_, err := mcpapps.CallTool(context.Background(), fakeAccountServer(t), "explodes", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "mailbox unreachable")
}

func TestCallTool_CancelledContextFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := mcpapps.CallTool(ctx, fakeAccountServer(t), "list_accounts", nil)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}
