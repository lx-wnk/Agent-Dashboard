package pluginsettings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
	"github.com/lx-wnk/agent-dashboard/server/internal/secretbox"
)

type fakeRepo struct{ rows map[string]row }
type row struct {
	value, nonce string
	secret       bool
	pluginID     string
}

func (f *fakeRepo) ListByPlugin(_ context.Context, pluginID string) ([]Stored, error) {
	out := []Stored{}
	for k, r := range f.rows {
		if r.pluginID != "" && r.pluginID != pluginID {
			continue
		}
		out = append(out, Stored{Key: k, Value: r.value, Nonce: r.nonce, Secret: r.secret})
	}
	return out, nil
}
func (f *fakeRepo) Upsert(_ context.Context, pluginID string, s Stored) error {
	f.rows[s.Key] = row{s.Value, s.Nonce, s.Secret, pluginID}
	return nil
}
func (f *fakeRepo) DeleteByPlugin(_ context.Context, _ string) error {
	f.rows = map[string]row{}
	return nil
}

func TestService_PutEncryptsSecret_GetMasks(t *testing.T) {
	box, _ := secretbox.New(make([]byte, 32))
	repo := &fakeRepo{rows: map[string]row{}}
	svc := New(repo, box)
	schema := []plugin.SettingField{
		{Key: "endpoint", Type: "url"},
		{Key: "apiKey", Type: "string", Secret: true},
	}
	ctx := context.Background()

	require.NoError(t, svc.Put(ctx, "p1", schema, map[string]string{"endpoint": "https://x", "apiKey": "KEY123"}))

	// stored secret is encrypted (not plaintext)
	assert.NotEqual(t, "KEY123", repo.rows["apiKey"].value)
	assert.True(t, repo.rows["apiKey"].secret)

	// Get masks secrets, shows non-secret values
	view, err := svc.Get(ctx, "p1", schema)
	require.NoError(t, err)
	assert.Equal(t, "https://x", view["endpoint"])
	assert.Equal(t, MaskedSentinel, view["apiKey"])

	// PUT with the sentinel leaves the secret unchanged
	require.NoError(t, svc.Put(ctx, "p1", schema, map[string]string{"apiKey": MaskedSentinel}))
	dec, err := svc.Decrypted(ctx, "p1", schema)
	require.NoError(t, err)
	assert.Equal(t, "KEY123", dec["apiKey"])
}

func TestService_DecryptedAll_ReturnsPlaintext(t *testing.T) {
	box, _ := secretbox.New(make([]byte, 32))
	repo := &fakeRepo{rows: map[string]row{}}
	svc := New(repo, box)
	schema := []plugin.SettingField{
		{Key: "endpoint", Type: "url"},
		{Key: "apiKey", Type: "string", Secret: true},
	}
	ctx := context.Background()

	require.NoError(t, svc.Put(ctx, "p1", schema,
		map[string]string{"endpoint": "https://x", "apiKey": "TOPSECRET"}))

	all, err := svc.DecryptedAll(ctx, "p1")
	require.NoError(t, err)
	assert.Equal(t, "https://x", all["endpoint"])
	assert.Equal(t, "TOPSECRET", all["apiKey"], "secret value must be decrypted")

	// Plugin not found → empty map, no error.
	empty, err := svc.DecryptedAll(ctx, "unknown-plugin")
	require.NoError(t, err)
	assert.Empty(t, empty)
}
