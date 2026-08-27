package repo_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func newGrantRepo(t *testing.T) (repo.GrantRepo, context.Context) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	return repo.NewGrantRepo(bundle.Client), context.Background()
}

func TestGrantRequiresGrantedBy(t *testing.T) {
	r, ctx := newGrantRepo(t)
	_, err := r.Create(ctx, repo.CreateGrantInput{
		CapabilityName: "Bash",
		Context:        repo.GrantContextFor(repo.GrantContextGlobal, ""),
		Mode:           repo.GrantModeAllow,
		Pattern:        "git status*",
		// GrantedBy deliberately omitted
	})
	if err == nil {
		t.Fatal("a grant without granted_by must be refused — identity on a decision is not optional")
	}
}

func TestGrantRevokeIsATombstone(t *testing.T) {
	r, ctx := newGrantRepo(t)
	g, err := r.Create(ctx, repo.CreateGrantInput{
		CapabilityName: "Bash",
		Context:        repo.GrantContextFor(repo.GrantContextGlobal, ""),
		Mode:           repo.GrantModeAllow,
		Pattern:        "git status*",
		GrantedBy:      "user:alex",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := r.Revoke(ctx, g.ID, "user:alex"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	all, err := r.ListForCapability(ctx, "Bash")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("revoke must not delete the row, got %d rows", len(all))
	}
	if all[0].RevokedAt == nil {
		t.Error("revoked_at must be set — revocation is a tombstone, not a delete")
	}
}

func TestGrantCreateRejectsInvalidPattern(t *testing.T) {
	r, ctx := newGrantRepo(t)
	_, err := r.Create(ctx, repo.CreateGrantInput{
		CapabilityName: "mail.send",
		Context:        repo.GrantContextFor(repo.GrantContextGlobal, ""),
		Mode:           repo.GrantModeAllow,
		Pattern:        "domain:",
		GrantedBy:      "user:alex",
	})
	if err == nil {
		t.Fatal("a grant with an invalid pattern must be refused — a malformed pattern that is stored and never matches is a silent deny")
	}
}
