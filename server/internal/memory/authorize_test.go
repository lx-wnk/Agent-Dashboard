package memory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/memory"
)

// newAuthorizeGateForTest wires a memory.Gate against a real in-memory
// SQLite database, with the capability catalogue seeded so a grant actually
// resolves to allow rather than the fail-closed "capability never
// catalogued" default. asker is passed straight through as Gate.Asker so
// tests can assert both the nil (fail-closed) and wired-asker paths.
func newAuthorizeGateForTest(t *testing.T, asker capability.Asker) (memory.Gate, context.Context) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	capRepo := repo.NewCapabilityRepo(bundle.Client)
	repo.SeedCapabilities(context.Background(), capRepo)
	gate := memory.Gate{
		Capabilities: capRepo,
		Grants:       repo.NewGrantRepo(bundle.Client),
		GrantUsage:   repo.NewGrantUsageRepo(bundle.Client, bundle.WriteClient),
		Asker:        asker,
	}
	return gate, context.Background()
}

// TestAuthorizeEnforcesGrantRateLimit is the regression test for the
// "rate-limited memory grant is silently unlimited" bug: Authorize used to
// pass capability.GrantView{}, 0 to Enforce regardless of what the winning
// grant actually configured, so a LimitCount was accepted at grant-creation
// time and then never read back. With the fix, the second call inside the
// same window must be refused (Enforce downgrades to ask, and with no Asker
// wired that fails closed to an error) rather than silently allowed forever.
func TestAuthorizeEnforcesGrantRateLimit(t *testing.T) {
	gate, ctx := newAuthorizeGateForTest(t, nil)
	_, err := gate.Grants.Create(ctx, repo.CreateGrantInput{
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
	err = gate.Authorize(ctx, repo.CapabilityMemoryRead, "", scope)
	require.NoError(t, err, "first call must be within the limit")

	err = gate.Authorize(ctx, repo.CapabilityMemoryRead, "", scope)
	require.Error(t, err, "second call within the same window must be refused by the grant's own rate limit")
}

// TestAuthorizeUnlimitedGrantNeverTouchesUsage exercises the LimitCount 0
// (unlimited) path repeatedly to guard against a regression that starts
// recording usage — and therefore eventually rate-limiting — a grant that
// was never configured with a limit at all.
func TestAuthorizeUnlimitedGrantNeverTouchesUsage(t *testing.T) {
	gate, ctx := newAuthorizeGateForTest(t, nil)
	_, err := gate.Grants.Create(ctx, repo.CreateGrantInput{
		CapabilityName: repo.CapabilityMemoryRead,
		Context:        repo.GrantContextFor(repo.GrantContextGlobal, ""),
		Pattern:        "",
		Mode:           repo.GrantModeAllow,
		GrantedBy:      "test",
	})
	require.NoError(t, err)

	scope := repo.GlobalScope()
	for i := 0; i < 5; i++ {
		err = gate.Authorize(ctx, repo.CapabilityMemoryRead, "", scope)
		require.NoError(t, err, "call %d: an unlimited grant must never be rate-limited", i)
	}
}

// recordingAsker mirrors capability_test's recordingAsker (unexported there,
// so it cannot be reused directly) to pin what Gate.Authorize hands its
// Asker: the capability name and value being asked about, not merely a
// resolved Decision.
type recordingAsker struct {
	answer      bool
	err         error
	called      bool
	lastRequest capability.Request
}

func (a *recordingAsker) Ask(_ context.Context, req capability.Request, _ capability.Decision) (bool, error) {
	a.called = true
	a.lastRequest = req
	return a.answer, a.err
}

// TestGateNilAskerFailsClosedOnAsk proves a Gate built without an Asker
// (the four non-interactive call sites: pipeline memory push, obsidian
// index) cannot block for a human — an ask-effect decision denies instead.
func TestGateNilAskerFailsClosedOnAsk(t *testing.T) {
	gate, ctx := newAuthorizeGateForTest(t, nil)
	_, err := gate.Grants.Create(ctx, repo.CreateGrantInput{
		CapabilityName: repo.CapabilityMemoryRead,
		Context:        repo.GrantContextFor(repo.GrantContextGlobal, ""),
		Pattern:        "",
		Mode:           repo.GrantModeAsk,
		GrantedBy:      "test",
	})
	require.NoError(t, err)

	err = gate.Authorize(ctx, repo.CapabilityMemoryRead, "", repo.GlobalScope())
	require.ErrorIs(t, err, capability.ErrAskRequired)
}

// TestGateAskerGrantedAllowsAndSeesTheRequest proves a wired Asker that
// answers yes lets the call through, and that it receives the capability
// name and value being asked about — the whole point of Problem 1.
func TestGateAskerGrantedAllowsAndSeesTheRequest(t *testing.T) {
	asker := &recordingAsker{answer: true}
	gate, ctx := newAuthorizeGateForTest(t, asker)
	_, err := gate.Grants.Create(ctx, repo.CreateGrantInput{
		CapabilityName: repo.CapabilityMemoryRead,
		Context:        repo.GrantContextFor(repo.GrantContextGlobal, ""),
		Pattern:        "",
		Mode:           repo.GrantModeAsk,
		GrantedBy:      "test",
	})
	require.NoError(t, err)

	err = gate.Authorize(ctx, repo.CapabilityMemoryRead, "some-value", repo.GlobalScope())
	require.NoError(t, err)
	require.True(t, asker.called, "an ask decision must consult the wired asker")
	require.Equal(t, repo.CapabilityMemoryRead, asker.lastRequest.Capability)
	require.Equal(t, "some-value", asker.lastRequest.Value)
}

// TestGateAskerRefusedDenies proves a wired Asker that answers no denies the
// call with capability.ErrDenied, not a silent pass-through.
func TestGateAskerRefusedDenies(t *testing.T) {
	asker := &recordingAsker{answer: false}
	gate, ctx := newAuthorizeGateForTest(t, asker)
	_, err := gate.Grants.Create(ctx, repo.CreateGrantInput{
		CapabilityName: repo.CapabilityMemoryRead,
		Context:        repo.GrantContextFor(repo.GrantContextGlobal, ""),
		Pattern:        "",
		Mode:           repo.GrantModeAsk,
		GrantedBy:      "test",
	})
	require.NoError(t, err)

	err = gate.Authorize(ctx, repo.CapabilityMemoryRead, "", repo.GlobalScope())
	require.ErrorIs(t, err, capability.ErrDenied)
}

// TestGateAskerErrorSurfaces proves an asker transport failure is returned
// to the caller rather than silently treated as a denial.
func TestGateAskerErrorSurfaces(t *testing.T) {
	sentinel := errors.New("asker transport failed")
	asker := &recordingAsker{err: sentinel}
	gate, ctx := newAuthorizeGateForTest(t, asker)
	_, err := gate.Grants.Create(ctx, repo.CreateGrantInput{
		CapabilityName: repo.CapabilityMemoryRead,
		Context:        repo.GrantContextFor(repo.GrantContextGlobal, ""),
		Pattern:        "",
		Mode:           repo.GrantModeAsk,
		GrantedBy:      "test",
	})
	require.NoError(t, err)

	err = gate.Authorize(ctx, repo.CapabilityMemoryRead, "", repo.GlobalScope())
	require.ErrorIs(t, err, sentinel)
}
