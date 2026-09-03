package mcp_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/mcp"
)

func newIssuer(t *testing.T) (mcp.StageKeyIssuer, repo.ApiKeyRepo, context.Context) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	keys := repo.NewApiKeyRepo(bundle.Client)
	return mcp.StageKeyIssuer{Keys: keys}, keys, context.Background()
}

func TestStageKeyIssuer_IssuedKeyResolvesAndCarriesAttribution(t *testing.T) {
	issuer, keys, ctx := newIssuer(t)

	token, err := issuer.Issue(ctx, "sr-1", 30*time.Minute)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(token, "mcp_"), "the token must be an ordinary MCP bearer")

	row, err := keys.GetByHash(ctx, mcp.HashToken(token))
	require.NoError(t, err)
	require.Equal(t, "sr-1", row.StageRunID)
	require.Equal(t, repo.ApiKeyKindStageRun, row.Kind)
	require.NotNil(t, row.ExpiresAt)
	require.WithinDuration(t, time.Now().Add(30*time.Minute+mcp.StageKeyTTLBuffer), *row.ExpiresAt, time.Minute)
}

// The scope set is fixed by design (spec D3). keys:manage is excluded on
// purpose: an agent that can mint keys can mint one with no stage run and
// escape its own attribution.
func TestStageKeyIssuer_NeverGrantsKeysManage(t *testing.T) {
	issuer, keys, ctx := newIssuer(t)

	token, err := issuer.Issue(ctx, "sr-1", time.Minute)
	require.NoError(t, err)
	row, err := keys.GetByHash(ctx, mcp.HashToken(token))
	require.NoError(t, err)

	require.NotContains(t, row.Scopes, "keys:manage")
	require.Contains(t, row.Scopes, "memory:read")
	require.Contains(t, row.Scopes, "obsidian:write")
}

func TestStageKeyIssuer_RevokeStopsTheKey(t *testing.T) {
	issuer, keys, ctx := newIssuer(t)

	token, err := issuer.Issue(ctx, "sr-1", time.Hour)
	require.NoError(t, err)
	_, err = keys.GetByHash(ctx, mcp.HashToken(token))
	require.NoError(t, err)

	require.NoError(t, issuer.Revoke(ctx, "sr-1"))

	_, err = keys.GetByHash(ctx, mcp.HashToken(token))
	require.Error(t, err)
}

// Revoking a stage run that never had a key is not an error: the orchestrator
// calls Revoke on every terminal transition, including runs whose spawn never
// got a credential.
func TestStageKeyIssuer_RevokeUnknownStageRunIsNotAnError(t *testing.T) {
	issuer, _, ctx := newIssuer(t)
	require.NoError(t, issuer.Revoke(ctx, "sr-never-existed"))
}

func TestStageKeyIssuer_TwoIssuesGiveDifferentTokens(t *testing.T) {
	issuer, _, ctx := newIssuer(t)

	a, err := issuer.Issue(ctx, "sr-1", time.Minute)
	require.NoError(t, err)
	b, err := issuer.Issue(ctx, "sr-1", time.Minute)
	require.NoError(t, err)
	require.NotEqual(t, a, b, "a retry must not reuse the previous run's token")
}
