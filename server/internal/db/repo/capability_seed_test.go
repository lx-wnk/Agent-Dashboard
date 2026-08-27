package repo_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/permissions"
)

func TestSeedCapabilitiesIsIdempotent(t *testing.T) {
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	ctx := context.Background()

	capRepo := repo.NewCapabilityRepo(bundle.Client)
	want := len(permissions.GrantableToolNames())

	seeded, err := repo.SeedCapabilities(ctx, capRepo)
	if err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if seeded != want {
		t.Errorf("first seed created %d rows, want %d (one per grantable tool)", seeded, want)
	}

	again, err := repo.SeedCapabilities(ctx, capRepo)
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if again != 0 {
		t.Errorf("second seed created %d rows, want 0 — it must be idempotent", again)
	}

	rows, err := capRepo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != want {
		t.Errorf("catalogue has %d rows, want %d", len(rows), want)
	}
}

func TestSeedCapabilitiesClassesAndEnforcement(t *testing.T) {
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	ctx := context.Background()

	capRepo := repo.NewCapabilityRepo(bundle.Client)
	if _, err := repo.SeedCapabilities(ctx, capRepo); err != nil {
		t.Fatalf("seed: %v", err)
	}

	webFetch, err := capRepo.Get(ctx, "WebFetch")
	if err != nil {
		t.Fatalf("get WebFetch: %v", err)
	}
	if webFetch.Class != repo.CapClassReach {
		t.Errorf("WebFetch class = %q, want %q", webFetch.Class, repo.CapClassReach)
	}

	bash, err := capRepo.Get(ctx, "Bash")
	if err != nil {
		t.Fatalf("get Bash: %v", err)
	}
	if bash.Class != repo.CapClassTool {
		t.Errorf("Bash class = %q, want %q", bash.Class, repo.CapClassTool)
	}

	for _, row := range []*struct {
		name string
		by   []string
	}{
		{"Bash", bash.EnforceableBy},
		{"WebFetch", webFetch.EnforceableBy},
	} {
		want := map[string]bool{capability.EnforcerSpawn: true, capability.EnforcerHook: true}
		if len(row.by) != len(want) {
			t.Errorf("%s enforceable_by = %v, want %v", row.name, row.by, want)
			continue
		}
		for _, v := range row.by {
			if !want[v] {
				t.Errorf("%s enforceable_by contains unexpected %q", row.name, v)
			}
		}
	}
}

// TestSeedCapabilitiesDoesNotOverwriteHumanEdit proves a class change made
// through the catalogue after seeding survives a second boot's seed pass.
func TestSeedCapabilitiesDoesNotOverwriteHumanEdit(t *testing.T) {
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	ctx := context.Background()

	capRepo := repo.NewCapabilityRepo(bundle.Client)
	if _, err := repo.SeedCapabilities(ctx, capRepo); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// A human edits Bash's class through the catalogue after seeding.
	if _, err := capRepo.Upsert(ctx, repo.UpsertCapabilityInput{
		Name:          "Bash",
		Class:         repo.CapClassResource,
		EnforceableBy: []string{capability.EnforcerServer},
	}); err != nil {
		t.Fatalf("human edit: %v", err)
	}

	if _, err := repo.SeedCapabilities(ctx, capRepo); err != nil {
		t.Fatalf("re-seed: %v", err)
	}

	bash, err := capRepo.Get(ctx, "Bash")
	if err != nil {
		t.Fatalf("get Bash: %v", err)
	}
	if bash.Class != repo.CapClassResource {
		t.Errorf("Bash class = %q, want %q (human edit must survive a re-seed)", bash.Class, repo.CapClassResource)
	}
}
