package repo

import (
	"context"
	"log/slog"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/permissions"
)

// DefaultCapabilityView is the class and enforcement-point set a grantable
// tool name gets when it has no catalogue row yet: class "tool" for
// everything except WebFetch ("reach"), enforceable at {spawn, hook} — both
// points see tool calls.
//
// This is the single place that classification lives. SeedCapabilities below
// and pipeline's capabilityViewFor fallback (server/internal/pipeline/spawner.go)
// both call it instead of each holding their own copy of the same literals —
// two independent copies could silently drift apart; one shared function
// turns that into a compile error instead.
func DefaultCapabilityView(name string) capability.CapabilityView {
	class := CapClassTool
	if name == "WebFetch" {
		class = CapClassReach
	}
	return capability.CapabilityView{
		Name:          name,
		Class:         class,
		EnforceableBy: []string{capability.EnforcerSpawn, capability.EnforcerHook},
	}
}

// SeedCapabilities gives every grantable tool name a capability catalogue
// row. Without this, the capabilities table stays empty and every lookup
// resolves to a zero-value CapabilityView — an unrecognised class, which
// defaultEffect sends to deny. Seeding is what makes the gate's fail-closed
// default apply only to capabilities that were never named, not to every
// capability that exists.
//
// Names come from permissions.GrantableToolNames, the same set
// permissions.IsAllowedTool reads, so the catalogue cannot drift from the
// allow-list. Class and enforcement come from DefaultCapabilityView.
//
// Idempotent: a name that already has a row is left untouched rather than
// upserted, so a human who has since edited a row's class through the
// catalogue is not overwritten on the next boot. Only missing rows are
// created. A name whose insert fails is logged and skipped rather than
// aborting the loop — one bad name must not stop every name ordered after it.
func SeedCapabilities(ctx context.Context, capabilities CapabilityRepo) (int, error) {
	seeded, skipped := 0, 0
	for _, name := range permissions.GrantableToolNames() {
		// "not found" is detected via ent.IsNotFound unwrapping through Get's
		// fmt.Errorf("capability.Get: %w", err) — errors.As traverses that
		// wrap today. If Get's error wrapping ever changes to something
		// ent.IsNotFound can no longer see through, every name silently takes
		// the "skip and warn" branch below instead of being seeded.
		if _, err := capabilities.Get(ctx, name); err == nil {
			continue // already catalogued — do not overwrite a class a human may have changed
		} else if !ent.IsNotFound(err) {
			skipped++
			slog.Warn("seed capabilities: skipped", "name", name, "err", err)
			continue
		}

		view := DefaultCapabilityView(name)
		if _, err := capabilities.Upsert(ctx, UpsertCapabilityInput{
			Name:          view.Name,
			Class:         view.Class,
			EnforceableBy: view.EnforceableBy,
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
