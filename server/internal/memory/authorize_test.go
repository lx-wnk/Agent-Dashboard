package memory_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/memory"
)

// newAuthorizeDepsForTest wires the three repos memory.Authorize needs
// against a real in-memory SQLite database, with the capability catalogue
// seeded so a grant actually resolves to allow rather than the fail-closed
// "capability never catalogued" default.
func newAuthorizeDepsForTest(t *testing.T) (repo.CapabilityRepo, repo.GrantRepo, repo.GrantUsageRepo, context.Context) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	capRepo := repo.NewCapabilityRepo(bundle.Client)
	repo.SeedCapabilities(context.Background(), capRepo)
	return capRepo, repo.NewGrantRepo(bundle.Client), repo.NewGrantUsageRepo(bundle.Client, bundle.WriteClient), context.Background()
}

// TestAuthorizeEnforcesGrantRateLimit is the regression test for the
// "rate-limited memory grant is silently unlimited" bug: Authorize used to
// pass capability.GrantView{}, 0 to Enforce regardless of what the winning
// grant actually configured, so a LimitCount was accepted at grant-creation
// time and then never read back. With the fix, the second call inside the
// same window must be refused (Enforce downgrades to ask, and with no Asker
// wired that fails closed to an error) rather than silently allowed forever.
func TestAuthorizeEnforcesGrantRateLimit(t *testing.T) {
	capRepo, grants, grantUsage, ctx := newAuthorizeDepsForTest(t)
	_, err := grants.Create(ctx, repo.CreateGrantInput{
		CapabilityName:     repo.CapabilityMemoryRead,
		Context:            repo.GrantContextFor(repo.GrantContextGlobal, ""),
		Pattern:            "",
		Mode:               repo.GrantModeAllow,
		LimitCount:         1,
		LimitWindowSeconds: 3600,
		GrantedBy:          "test",
	})
	require.NoError(t, err)

	scope := repo.GlobalScope()
	err = memory.Authorize(ctx, capRepo, grants, grantUsage, repo.CapabilityMemoryRead, "", scope)
	require.NoError(t, err, "first call must be within the limit")

	err = memory.Authorize(ctx, capRepo, grants, grantUsage, repo.CapabilityMemoryRead, "", scope)
	require.Error(t, err, "second call within the same window must be refused by the grant's own rate limit")
}

// TestAuthorizeUnlimitedGrantNeverTouchesUsage exercises the LimitCount 0
// (unlimited) path repeatedly to guard against a regression that starts
// recording usage — and therefore eventually rate-limiting — a grant that
// was never configured with a limit at all.
func TestAuthorizeUnlimitedGrantNeverTouchesUsage(t *testing.T) {
	capRepo, grants, grantUsage, ctx := newAuthorizeDepsForTest(t)
	_, err := grants.Create(ctx, repo.CreateGrantInput{
		CapabilityName: repo.CapabilityMemoryRead,
		Context:        repo.GrantContextFor(repo.GrantContextGlobal, ""),
		Pattern:        "",
		Mode:           repo.GrantModeAllow,
		GrantedBy:      "test",
	})
	require.NoError(t, err)

	scope := repo.GlobalScope()
	for i := 0; i < 5; i++ {
		err = memory.Authorize(ctx, capRepo, grants, grantUsage, repo.CapabilityMemoryRead, "", scope)
		require.NoError(t, err, "call %d: an unlimited grant must never be rate-limited", i)
	}
}
