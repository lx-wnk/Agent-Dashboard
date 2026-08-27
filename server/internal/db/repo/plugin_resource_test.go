package repo_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func TestReconcilePluginResourcesIsIdempotent(t *testing.T) {
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	ctx := context.Background()

	pluginRepo := repo.NewPluginRepo(bundle.Client)
	resourceRepo := repo.NewResourceRepo(bundle.Client)

	if _, err := pluginRepo.Upsert(ctx, repo.UpsertPluginInput{
		ID:      "github-oauth",
		Name:    "GitHub OAuth",
		Version: "1.0.0",
	}); err != nil {
		t.Fatalf("seed plugin: %v", err)
	}

	n, err := repo.ReconcilePluginResources(ctx, resourceRepo, bundle.Client)
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if n != 1 {
		t.Errorf("first reconcile linked %d plugins, want 1", n)
	}

	again, err := repo.ReconcilePluginResources(ctx, resourceRepo, bundle.Client)
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if again != 0 {
		t.Errorf("second reconcile linked %d plugins, want 0 — it must be idempotent", again)
	}

	res, err := resourceRepo.Get(ctx, repo.ResourceKindApplication, repo.GlobalScope(), "github-oauth")
	if err != nil {
		t.Fatalf("registry row missing after reconcile: %v", err)
	}
	if res.OriginRef != "github-oauth" {
		t.Errorf("origin_ref = %q, want the manifest id", res.OriginRef)
	}

	rows, err := resourceRepo.ListForKind(ctx, repo.ResourceKindApplication)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("expected exactly 1 application resource, got %d", len(rows))
	}
}
