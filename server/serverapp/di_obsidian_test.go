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

func TestBuildObsidianClient_PartialConfigIsAnError(t *testing.T) {
	svc := newSettingsServiceForTest(t)
	require.NoError(t, svc.Set(t.Context(), "obsidian.baseURL", "https://127.0.0.1:27124"))
	// vaultRoot deliberately left empty
	_, err := buildObsidianClient(t.Context(), svc)
	require.Error(t, err)
	// obsidian.NewClient names the missing field as "VaultRoot" (Go field
	// name casing), not the settings key "vaultRoot".
	assert.Contains(t, err.Error(), "VaultRoot")
}
