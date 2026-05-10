package db_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
)

func TestOpen_InMemory(t *testing.T) {
	client, err := db.Open(":memory:")
	require.NoError(t, err)
	require.NotNil(t, client)
	_ = client.Close()
}

func TestOpen_AutoMigrate(t *testing.T) {
	client, err := db.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	// Prove schema was created: insert a record into api_keys.
	_, err = client.ApiKey.Create().
		SetID("test-id").
		SetName("test").
		SetKeyHash("abc123").
		SetScopes([]string{"tasks:read"}).
		Save(t.Context())
	require.NoError(t, err)
}
