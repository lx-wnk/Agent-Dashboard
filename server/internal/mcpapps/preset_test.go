package mcpapps_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/schema"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/mcpapps"
)

func TestApplyPreset_GrantsKnownToolsOnceAndSkipsUnknown(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	ctx := context.Background()
	apps := repo.NewMCPApplicationRepo(bundle.Client)
	grants := repo.NewGrantRepo(bundle.Client)

	_, err = apps.Upsert(ctx, repo.UpsertMCPApplicationInput{ResourceID: "res-mail", ServerName: "mail"})
	require.NoError(t, err)
	require.NoError(t, apps.RecordCatalogue(ctx, "res-mail",
		[]schema.CatalogueTool{{Name: "search"}, {Name: "send"}}, "", time.Now()))
	app, err := apps.GetByResourceID(ctx, "res-mail")
	require.NoError(t, err)

	p := mcpapps.Preset{Confirmed: true, AllowForRoutine: []string{"search", "not_in_catalogue"}, DenyGlobal: []string{"send"}}

	first, err := mcpapps.ApplyPreset(ctx, grants, app, p, "routine-1", "test")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"mcp__mail__search", "mcp__mail__send"}, first.Created)
	require.Equal(t, []string{"not_in_catalogue"}, first.Skipped)

	second, err := mcpapps.ApplyPreset(ctx, grants, app, p, "routine-1", "test")
	require.NoError(t, err)
	require.Empty(t, second.Created, "applying twice must not duplicate grants")
	require.ElementsMatch(t, []string{"mcp__mail__search", "mcp__mail__send"}, second.Existing)

	rows, err := grants.ListForCapability(ctx, "mcp__mail__send")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "deny", rows[0].Mode)
	require.Equal(t, repo.GrantContextGlobal, rows[0].ContextKind)
}

func TestLoadPreset_ProbeOutputIsEmbedded(t *testing.T) {
	p, err := mcpapps.LoadPreset("imap-mcp-server")
	require.NoError(t, err)
	require.NotEmpty(t, p.DenyGlobal, "the preset must deny at least the send tools")
}

func TestApplyPreset_RefusesUnconfirmedPreset(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	ctx := context.Background()
	apps := repo.NewMCPApplicationRepo(bundle.Client)
	grants := repo.NewGrantRepo(bundle.Client)

	_, err = apps.Upsert(ctx, repo.UpsertMCPApplicationInput{ResourceID: "res-mail", ServerName: "mail"})
	require.NoError(t, err)
	require.NoError(t, apps.RecordCatalogue(ctx, "res-mail",
		[]schema.CatalogueTool{{Name: "search"}, {Name: "send"}}, "", time.Now()))
	app, err := apps.GetByResourceID(ctx, "res-mail")
	require.NoError(t, err)

	p := mcpapps.Preset{Confirmed: false, AllowForRoutine: []string{"search"}, DenyGlobal: []string{"send"}}

	_, err = mcpapps.ApplyPreset(ctx, grants, app, p, "routine-1", "test")
	require.ErrorIs(t, err, mcpapps.ErrPresetUnconfirmed)

	rows, err := grants.List(ctx)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestApplyPreset_NarrowerGrantIsNotExisting(t *testing.T) {
	future := time.Now().Add(time.Hour)
	tests := []struct {
		name   string
		in     repo.CreateGrantInput
		revoke bool
	}{
		{
			name:   "revoked",
			in:     repo.CreateGrantInput{},
			revoke: true,
		},
		{
			name: "expires in the future",
			in:   repo.CreateGrantInput{ExpiresAt: &future},
		},
		{
			name: "narrower pattern",
			in:   repo.CreateGrantInput{Pattern: "domain:example.com"},
		},
		{
			name: "rate limited",
			in:   repo.CreateGrantInput{LimitCount: 5, LimitWindowSeconds: 60},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle, err := db.Open(":memory:")
			require.NoError(t, err)
			t.Cleanup(func() { _ = bundle.Client.Close() })
			ctx := context.Background()
			apps := repo.NewMCPApplicationRepo(bundle.Client)
			grants := repo.NewGrantRepo(bundle.Client)

			_, err = apps.Upsert(ctx, repo.UpsertMCPApplicationInput{ResourceID: "res-mail", ServerName: "mail"})
			require.NoError(t, err)
			require.NoError(t, apps.RecordCatalogue(ctx, "res-mail",
				[]schema.CatalogueTool{{Name: "search"}}, "", time.Now()))
			app, err := apps.GetByResourceID(ctx, "res-mail")
			require.NoError(t, err)

			in := tt.in
			in.CapabilityName = "mcp__mail__search"
			in.Context = repo.GrantContextFor(repo.GrantContextRoutine, "routine-1")
			in.Mode = "allow"
			in.GrantedBy = "test"
			row, err := grants.Create(ctx, in)
			require.NoError(t, err)
			if tt.revoke {
				require.NoError(t, grants.Revoke(ctx, row.ID, "test"))
			}

			p := mcpapps.Preset{Confirmed: true, AllowForRoutine: []string{"search"}}
			res, err := mcpapps.ApplyPreset(ctx, grants, app, p, "routine-1", "test")
			require.NoError(t, err)
			require.Contains(t, res.Created, "mcp__mail__search")
			require.NotContains(t, res.Existing, "mcp__mail__search")
		})
	}
}
