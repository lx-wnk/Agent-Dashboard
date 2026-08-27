package repo

import (
	"context"
	"log/slog"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/permissions"
)

// SeedCapabilities gives every grantable tool name a capability catalogue
// row. Without this, the capabilities table stays empty and every lookup
// resolves to a zero-value CapabilityView — an unrecognised class, which
// defaultEffect sends to deny. Seeding is what makes the gate's fail-closed
// default apply only to capabilities that were never named, not to every
// capability that exists.
//
// Names come from permissions.GrantableToolNames, the same set
// permissions.IsAllowedTool reads, so the catalogue cannot drift from the
// allow-list. Every seeded capability is class "tool", except WebFetch which
// is "reach" — the only two classes with a real capability behind them today.
// EnforceableBy is {spawn, hook} for all of them: both points see tool calls.
//
// Idempotent: a name that already has a row is left untouched rather than
// upserted, so a human who has since edited a row's class through the
// catalogue is not overwritten on the next boot. Only missing rows are
// created. A name whose insert fails is logged and skipped rather than
// aborting the loop — one bad name must not stop every name ordered after it.
func SeedCapabilities(ctx context.Context, capabilities CapabilityRepo) (int, error) {
	seeded, skipped := 0, 0
	for _, name := range permissions.GrantableToolNames() {
		if _, err := capabilities.Get(ctx, name); err == nil {
			continue // already catalogued — do not overwrite a class a human may have changed
		} else if !ent.IsNotFound(err) {
			skipped++
			slog.Warn("seed capabilities: skipped", "name", name, "err", err)
			continue
		}

		class := CapClassTool
		if name == "WebFetch" {
			class = CapClassReach
		}
		if _, err := capabilities.Upsert(ctx, UpsertCapabilityInput{
			Name:          name,
			Class:         class,
			EnforceableBy: []string{capability.EnforcerSpawn, capability.EnforcerHook},
		}); err != nil {
			skipped++
			slog.Warn("seed capabilities: skipped", "name", name, "err", err)
			continue
		}
		seeded++
	}
	if skipped > 0 {
		slog.Warn("seed capabilities: some names were not seeded", "seeded", seeded, "skipped", skipped)
	}
	return seeded, nil
}
