package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBStore_SetGetList(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	store, err := openDBStore(dbPath)
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()

	require.NoError(t, store.Set(ctx, "auth.mode", "plugin"))
	require.NoError(t, store.Set(ctx, "auth.mode", "none")) // upsert

	v, ok, err := store.Get(ctx, "auth.mode")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "none", v)

	all, err := store.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, "none", all["auth.mode"])
}

func TestDBStore_RejectsUnknownKey(t *testing.T) {
	store, err := openDBStore(t.TempDir() + "/test.db")
	require.NoError(t, err)
	defer store.Close()
	// CLI validates against the registry before writing.
	require.Error(t, store.SetValidated(context.Background(), "nope", "x"))
	require.Error(t, store.SetValidated(context.Background(), "spawn.rateLimit", "abc"))
	require.NoError(t, store.SetValidated(context.Background(), "spawn.rateLimit", "7"))
}
