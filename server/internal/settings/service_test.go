package settings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/secretbox"
)

type fakeRepo struct {
	values map[string]string
	nonces map[string]string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{values: map[string]string{}, nonces: map[string]string{}}
}

func (f *fakeRepo) Get(_ context.Context, k string) (string, bool, error) {
	v, ok := f.values[k]
	return v, ok, nil
}
func (f *fakeRepo) Set(_ context.Context, k, v string) error { f.values[k] = v; return nil }
func (f *fakeRepo) SetSecret(_ context.Context, k, ciphertext, nonce string) error {
	f.values[k] = ciphertext
	f.nonces[k] = nonce
	return nil
}
func (f *fakeRepo) GetSecret(_ context.Context, k string) (string, string, bool, error) {
	v, ok := f.values[k]
	if !ok {
		return "", "", false, nil
	}
	return v, f.nonces[k], true, nil
}
func (f *fakeRepo) ListAll(_ context.Context) (map[string]string, error) {
	out := make(map[string]string, len(f.values))
	for k, v := range f.values {
		out[k] = v
	}
	return out, nil
}

func TestService_DefaultsAndTypedAccess(t *testing.T) {
	svc := New(newFakeRepo(), nil)
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

func TestService_SecretRoundTripAndMasking(t *testing.T) {
	box, err := secretbox.New(make([]byte, 32))
	require.NoError(t, err)
	repo := newFakeRepo()
	svc := New(repo, box)

	require.NoError(t, svc.Load(t.Context()))

	require.NoError(t, svc.Set(t.Context(), "obsidian.apiKey", "sk-live-123"))

	// Stored ciphertext must not be the plaintext.
	assert.NotEqual(t, "sk-live-123", repo.values["obsidian.apiKey"])
	assert.NotEmpty(t, repo.nonces["obsidian.apiKey"])

	// Reading it back decrypts.
	got, err := svc.Secret(t.Context(), "obsidian.apiKey")
	require.NoError(t, err)
	assert.Equal(t, "sk-live-123", got)

	// Every non-decrypting read path masks.
	assert.Equal(t, secretbox.MaskedSentinel, svc.String("obsidian.apiKey"))
	assert.Equal(t, secretbox.MaskedSentinel, svc.Effective()["obsidian.apiKey"])

	// Re-submitting the mask leaves the stored value untouched, so a UI that
	// round-trips what it was shown cannot overwrite the real secret.
	before := repo.values["obsidian.apiKey"]
	require.NoError(t, svc.Set(t.Context(), "obsidian.apiKey", secretbox.MaskedSentinel))
	assert.Equal(t, before, repo.values["obsidian.apiKey"])
}
