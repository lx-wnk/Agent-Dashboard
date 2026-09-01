package serverapp

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/secretbox"
	"github.com/lx-wnk/agent-dashboard/server/internal/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSettingsServiceForTest returns a settings service over a fresh in-memory
// database with a deterministic box, so a secret written in a test is readable
// back in the same test.
func newSettingsServiceForTest(t *testing.T) *settings.Service {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	box, err := secretbox.New(make([]byte, 32))
	require.NoError(t, err)
	svc := settings.New(settingsRepoAdapter{inner: repo.NewAppSettingRepo(bundle.Client)}, box)
	require.NoError(t, svc.Load(t.Context()))
	return svc
}

func TestBuildObsidianClient_UnconfiguredIsNotAnError(t *testing.T) {
	svc := newSettingsServiceForTest(t) // nothing set
	client, err := buildObsidianClient(t.Context(), svc)
	require.NoError(t, err)
	assert.Nil(t, client, "an unconfigured vault must disable the feature, not fail the boot")
}

// baseURL, vaultRoot and apiKey are a required trio: if any one is set, all
// three must be, or a half-configured vault would build a client that looks
// ready and then fails every request with an unhelpful 401. Each of the
// three tests below pins exactly one missing member so a regression in one
// check cannot hide behind the other two passing.

func TestBuildObsidianClient_MissingAPIKeyIsAnError(t *testing.T) {
	svc := newSettingsServiceForTest(t)
	require.NoError(t, svc.Set(t.Context(), "obsidian.baseURL", "https://127.0.0.1:27124"))
	require.NoError(t, svc.Set(t.Context(), "obsidian.vaultRoot", "notes"))
	// apiKey deliberately left unset
	_, err := buildObsidianClient(t.Context(), svc)
	require.Error(t, err, "a URL and root with no key must fail loudly, not ship an empty bearer token")
	assert.Contains(t, err.Error(), "obsidian.apiKey")
}

func TestBuildObsidianClient_MissingBaseURLIsAnError(t *testing.T) {
	svc := newSettingsServiceForTest(t)
	require.NoError(t, svc.Set(t.Context(), "obsidian.vaultRoot", "notes"))
	require.NoError(t, svc.Set(t.Context(), "obsidian.apiKey", "secret-key"))
	// baseURL deliberately left unset
	_, err := buildObsidianClient(t.Context(), svc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "obsidian.baseURL")
}

func TestBuildObsidianClient_MissingVaultRootIsAnError(t *testing.T) {
	svc := newSettingsServiceForTest(t)
	require.NoError(t, svc.Set(t.Context(), "obsidian.baseURL", "https://127.0.0.1:27124"))
	require.NoError(t, svc.Set(t.Context(), "obsidian.apiKey", "secret-key"))
	// vaultRoot deliberately left unset
	_, err := buildObsidianClient(t.Context(), svc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "obsidian.vaultRoot")
}
