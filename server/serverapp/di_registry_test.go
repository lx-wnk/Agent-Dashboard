package serverapp_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// TestReconcileOnBootLinksExistingPlugins proves the boot path leaves no plugin
// row without a registry identity. It exercises the repo layer directly rather
// than booting a server, because the assertion is about data, not wiring order.
func TestReconcileOnBootLinksExistingPlugins(t *testing.T) {
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	ctx := context.Background()

	plugins := repo.NewPluginRepo(bundle.Client)
	resources := repo.NewResourceRepo(bundle.Client)
	if _, err := plugins.Upsert(ctx, repo.UpsertPluginInput{
		ID:      "voice-whisper",
		Name:    "Whisper Voice",
		Version: "0.2.0",
	}); err != nil {
		t.Fatalf("seed plugin: %v", err)
	}

	if _, err := repo.ReconcilePluginResources(ctx, resources, bundle.Client); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if _, err := resources.Get(ctx, repo.ResourceKindApplication, repo.GlobalScope(), "voice-whisper"); err != nil {
		t.Errorf("plugin has no registry identity after boot reconcile: %v", err)
	}
}
