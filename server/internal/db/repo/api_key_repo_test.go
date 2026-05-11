package repo_test

import (
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

	key, err := r.Create(t.Context(), "my-key", "deadbeef", []string{"tasks:read"})
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

	_, err := r.Create(t.Context(), "k1", "hash1", []string{"tasks:read"})
	require.NoError(t, err)
	_, err = r.Create(t.Context(), "k2", "hash2", []string{"tasks:write"})
	require.NoError(t, err)

	keys, err := r.List(t.Context())
	require.NoError(t, err)
	require.Len(t, keys, 2)
}

func TestApiKeyRepo_Delete(t *testing.T) {
	client := openTestDB(t)
	r := repo.NewApiKeyRepo(client)

	key, err := r.Create(t.Context(), "to-delete", "hashX", nil)
	require.NoError(t, err)

	require.NoError(t, r.Delete(t.Context(), key.ID))

	keys, err := r.List(t.Context())
	require.NoError(t, err)
	require.Empty(t, keys)
}

func TestApiKeyRepo_TouchLastUsed(t *testing.T) {
	client := openTestDB(t)
	r := repo.NewApiKeyRepo(client)

	key, err := r.Create(t.Context(), "track", "hashT", nil)
	require.NoError(t, err)
	require.Nil(t, key.LastUsedAt)

	require.NoError(t, r.TouchLastUsed(t.Context(), key.ID))

	got, err := r.GetByHash(t.Context(), "hashT")
	require.NoError(t, err)
	require.NotNil(t, got.LastUsedAt)
	require.WithinDuration(t, time.Now(), *got.LastUsedAt, 2*time.Second)
}
