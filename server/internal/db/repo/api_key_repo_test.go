package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func openTestDB(t *testing.T) *ent.Client {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	return bundle.Client
}

func TestApiKeyRepo_CreateAndGetByHash(t *testing.T) {
	client := openTestDB(t)
	r := repo.NewApiKeyRepo(client)

	key, err := r.Create(t.Context(), repo.CreateApiKeyInput{Name: "my-key", Hash: "deadbeef", Scopes: []string{"tasks:read"}})
	require.NoError(t, err)
	require.Equal(t, "my-key", key.Name)
	require.Equal(t, "deadbeef", key.KeyHash)

	got, err := r.GetByHash(t.Context(), "deadbeef")
	require.NoError(t, err)
	require.Equal(t, key.ID, got.ID)
}

func TestApiKeyRepo_List(t *testing.T) {
	client := openTestDB(t)
	r := repo.NewApiKeyRepo(client)

	_, err := r.Create(t.Context(), repo.CreateApiKeyInput{Name: "k1", Hash: "hash1", Scopes: []string{"tasks:read"}})
	require.NoError(t, err)
	_, err = r.Create(t.Context(), repo.CreateApiKeyInput{Name: "k2", Hash: "hash2", Scopes: []string{"tasks:write"}})
	require.NoError(t, err)

	keys, err := r.List(t.Context())
	require.NoError(t, err)
	require.Len(t, keys, 2)
}

func TestApiKeyRepo_Delete(t *testing.T) {
	client := openTestDB(t)
	r := repo.NewApiKeyRepo(client)

	key, err := r.Create(t.Context(), repo.CreateApiKeyInput{Name: "to-delete", Hash: "hashX"})
	require.NoError(t, err)

	require.NoError(t, r.Delete(t.Context(), key.ID))

	keys, err := r.List(t.Context())
	require.NoError(t, err)
	require.Empty(t, keys)
}

func TestApiKeyRepo_TouchLastUsed(t *testing.T) {
	client := openTestDB(t)
	r := repo.NewApiKeyRepo(client)

	key, err := r.Create(t.Context(), repo.CreateApiKeyInput{Name: "track", Hash: "hashT"})
	require.NoError(t, err)
	require.Nil(t, key.LastUsedAt)

	require.NoError(t, r.TouchLastUsed(t.Context(), key.ID))

	got, err := r.GetByHash(t.Context(), "hashT")
	require.NoError(t, err)
	require.NotNil(t, got.LastUsedAt)
	require.WithinDuration(t, time.Now(), *got.LastUsedAt, 2*time.Second)
}

func TestApiKey_ExpiredKeyIsNotFoundByHash(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	r := repo.NewApiKeyRepo(bundle.Client)
	ctx := context.Background()

	past := time.Now().Add(-time.Minute)
	_, err = r.Create(ctx, repo.CreateApiKeyInput{
		Name: "expired", Hash: "hash-expired", Scopes: []string{"memory:read"},
		Kind: repo.ApiKeyKindStageRun, StageRunID: "sr-1", ExpiresAt: &past,
	})
	require.NoError(t, err)

	_, err = r.GetByHash(ctx, "hash-expired")
	require.Error(t, err, "an expired key must not resolve, the same way a revoked one does not")
}

func TestApiKey_UnexpiredStageRunKeyResolves(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	r := repo.NewApiKeyRepo(bundle.Client)
	ctx := context.Background()

	future := time.Now().Add(time.Hour)
	_, err = r.Create(ctx, repo.CreateApiKeyInput{
		Name: "live", Hash: "hash-live", Scopes: []string{"memory:read"},
		Kind: repo.ApiKeyKindStageRun, StageRunID: "sr-1", ExpiresAt: &future,
	})
	require.NoError(t, err)

	got, err := r.GetByHash(ctx, "hash-live")
	require.NoError(t, err)
	require.Equal(t, "sr-1", got.StageRunID)
	require.Equal(t, repo.ApiKeyKindStageRun, got.Kind)
}

func TestApiKey_UserKeyWithoutExpiryStillResolves(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	r := repo.NewApiKeyRepo(bundle.Client)
	ctx := context.Background()

	// Every row that existed before this change looks like this one.
	_, err = r.Create(ctx, repo.CreateApiKeyInput{
		Name: "human", Hash: "hash-human", Scopes: []string{"tasks:read"},
	})
	require.NoError(t, err)

	got, err := r.GetByHash(ctx, "hash-human")
	require.NoError(t, err)
	require.Equal(t, repo.ApiKeyKindUser, got.Kind, "the default kind must be user")
	require.Nil(t, got.ExpiresAt)
}

func TestApiKey_ListShowsOnlyUserKeys(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	r := repo.NewApiKeyRepo(bundle.Client)
	ctx := context.Background()

	_, err = r.Create(ctx, repo.CreateApiKeyInput{Name: "human", Hash: "h1", Scopes: []string{"tasks:read"}})
	require.NoError(t, err)
	future := time.Now().Add(time.Hour)
	_, err = r.Create(ctx, repo.CreateApiKeyInput{
		Name: "sr", Hash: "h2", Scopes: []string{"memory:read"},
		Kind: repo.ApiKeyKindStageRun, StageRunID: "sr-1", ExpiresAt: &future,
	})
	require.NoError(t, err)

	keys, err := r.List(ctx)
	require.NoError(t, err)
	require.Len(t, keys, 1, "an ephemeral key must not appear in the human-facing list")
	require.Equal(t, "human", keys[0].Name)
}

func TestApiKey_RevokeForStageRun(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	r := repo.NewApiKeyRepo(bundle.Client)
	ctx := context.Background()

	future := time.Now().Add(time.Hour)
	for _, h := range []string{"a", "b"} {
		_, err = r.Create(ctx, repo.CreateApiKeyInput{
			Name: h, Hash: h, Scopes: []string{"memory:read"},
			Kind: repo.ApiKeyKindStageRun, StageRunID: "sr-1", ExpiresAt: &future,
		})
		require.NoError(t, err)
	}
	_, err = r.Create(ctx, repo.CreateApiKeyInput{
		Name: "other", Hash: "c", Scopes: []string{"memory:read"},
		Kind: repo.ApiKeyKindStageRun, StageRunID: "sr-2", ExpiresAt: &future,
	})
	require.NoError(t, err)

	n, err := r.RevokeForStageRun(ctx, "sr-1")
	require.NoError(t, err)
	require.Equal(t, 2, n)

	_, err = r.GetByHash(ctx, "a")
	require.Error(t, err)
	_, err = r.GetByHash(ctx, "c")
	require.NoError(t, err, "another stage run's key must survive")
}

func TestApiKey_DeleteExpiredRemovesOnlyEphemeralRows(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	r := repo.NewApiKeyRepo(bundle.Client)
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	_, err = r.Create(ctx, repo.CreateApiKeyInput{
		Name: "old", Hash: "old", Scopes: []string{"memory:read"},
		Kind: repo.ApiKeyKindStageRun, StageRunID: "sr-1", ExpiresAt: &past,
	})
	require.NoError(t, err)
	_, err = r.Create(ctx, repo.CreateApiKeyInput{Name: "human", Hash: "human", Scopes: []string{"tasks:read"}})
	require.NoError(t, err)

	n, err := r.DeleteExpired(ctx, time.Now())
	require.NoError(t, err)
	require.Equal(t, 1, n)

	keys, err := r.List(ctx)
	require.NoError(t, err)
	require.Len(t, keys, 1, "a user key must never be swept, whatever its expiry")
}
