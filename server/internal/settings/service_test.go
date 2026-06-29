package settings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRepo struct{ m map[string]string }

func (f *fakeRepo) Get(_ context.Context, k string) (string, bool, error) {
	v, ok := f.m[k]
	return v, ok, nil
}
func (f *fakeRepo) Set(_ context.Context, k, v string) error { f.m[k] = v; return nil }
func (f *fakeRepo) ListAll(_ context.Context) (map[string]string, error) { return f.m, nil }

func TestService_DefaultsAndTypedAccess(t *testing.T) {
	svc := New(&fakeRepo{m: map[string]string{}})
	require.NoError(t, svc.Load(context.Background()))

	// registry default when no DB row
	assert.Equal(t, 5, svc.Int("spawn.rateLimit"))
	assert.True(t, svc.Bool("worktree.force"))
	assert.Equal(t, "none", svc.String("auth.mode"))
	assert.Empty(t, svc.StringSlice("spawn.allowedCommands"))

	// Set persists + updates snapshot, validated
	require.NoError(t, svc.Set(context.Background(), "worktree.force", "true"))
	assert.True(t, svc.Bool("worktree.force"))

	// invalid value rejected
	require.Error(t, svc.Set(context.Background(), "spawn.rateLimit", "abc"))
	// unknown key rejected
	require.Error(t, svc.Set(context.Background(), "nope", "1"))

	// stringSlice round-trips
	require.NoError(t, svc.Set(context.Background(), "spawn.allowedCommands", "voice-whisper,voice-webspeech"))
	assert.Equal(t, []string{"voice-whisper", "voice-webspeech"}, svc.StringSlice("spawn.allowedCommands"))
}
