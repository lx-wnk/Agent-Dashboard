package repo_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func TestAppSettingRepo_UpsertGetList(t *testing.T) {
	client := openTestDB(t)
	r := repo.NewAppSettingRepo(client)
	ctx := t.Context()

	_, err := r.Upsert(ctx, "auth.mode", "plugin")
	require.NoError(t, err)
	// upsert again updates value, not duplicates
	_, err = r.Upsert(ctx, "auth.mode", "none")
	require.NoError(t, err)

	v, ok, err := r.Get(ctx, "auth.mode")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "none", v)

	_, ok, err = r.Get(ctx, "missing.key")
	require.NoError(t, err)
	assert.False(t, ok)

	all, err := r.List(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 1)
}
