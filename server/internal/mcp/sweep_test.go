package mcp_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/mcp"
)

func TestSweepExpiredKeys_BootSweepRemovesExpiredEphemeralKeys(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	keys := repo.NewApiKeyRepo(bundle.Client)
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	_, err = keys.Create(ctx, repo.CreateApiKeyInput{
		Name: "old", Hash: "old", Scopes: mcp.StageRunScopes,
		Kind: repo.ApiKeyKindStageRun, StageRunID: "sr-1", ExpiresAt: &past,
	})
	require.NoError(t, err)
	_, err = keys.Create(ctx, repo.CreateApiKeyInput{Name: "human", Hash: "human", Scopes: []string{"tasks:read"}})
	require.NoError(t, err)

	// interval <= 0 runs the boot sweep only and returns.
	mcp.SweepExpiredKeys(ctx, keys, 0)

	remaining, err := keys.List(ctx)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.Equal(t, "human", remaining[0].Name)
}

func TestSweepExpiredKeys_StopsWhenTheContextIsCancelled(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		mcp.SweepExpiredKeys(ctx, repo.NewApiKeyRepo(bundle.Client), time.Hour)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SweepExpiredKeys did not return on a cancelled context")
	}
}
