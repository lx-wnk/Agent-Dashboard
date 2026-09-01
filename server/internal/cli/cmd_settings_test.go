package cli

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/secretbox"
)

// isolateMasterKeyPaths keeps LoadOrGenerateMasterKey (called from
// openDBStore) off the developer's real ~/.claude/dashboard-secret.key.
// CLAUDE_CONFIG_DIR alone is not enough: its legacy-key fallback still reads
// HOME/.claude when the primary configured path has no key yet, so both must
// be isolated to a temp dir together.
func isolateMasterKeyPaths(t *testing.T) {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
}

func TestDBStore_SetGetList(t *testing.T) {
	isolateMasterKeyPaths(t)
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
	isolateMasterKeyPaths(t)
	store, err := openDBStore(t.TempDir() + "/test.db")
	require.NoError(t, err)
	defer store.Close()
	// CLI validates against the registry before writing.
	require.Error(t, store.SetValidated(context.Background(), "nope", "x"))
	require.Error(t, store.SetValidated(context.Background(), "spawn.rateLimit", "abc"))
	require.NoError(t, store.SetValidated(context.Background(), "spawn.rateLimit", "7"))
}

// TestDBStore_SetValidated_EncryptsSecretValue guards the CLI's lockout
// recovery path: a secret set through the CLI must land encrypted, mirroring
// settings.Service's own round-trip test (internal/settings/service_test.go
// TestService_SecretRoundTripAndMasking, assert.NotEqual on the stored row).
func TestDBStore_SetValidated_EncryptsSecretValue(t *testing.T) {
	isolateMasterKeyPaths(t)
	store, err := openDBStore(t.TempDir() + "/test.db")
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()

	require.NoError(t, store.SetValidated(ctx, "obsidian.apiKey", "sk-live-999"))

	all, err := store.List(ctx)
	require.NoError(t, err)
	assert.NotEqual(t, "sk-live-999", all["obsidian.apiKey"])
	assert.NotEmpty(t, all["obsidian.apiKey"])
}

// TestDBStore_SetValidated_RefusesSecretWithoutMasterKey pins Finding A's
// second half: a secret write must never fall back to plaintext when the
// master key cannot be resolved. DASHBOARD_SECRET_KEY set to invalid hex
// forces the real resolution path in openDBStore to fail, exactly as it
// would if an operator misconfigured the env var during recovery.
func TestDBStore_SetValidated_RefusesSecretWithoutMasterKey(t *testing.T) {
	isolateMasterKeyPaths(t)
	t.Setenv("DASHBOARD_SECRET_KEY", "not-valid-hex")
	store, err := openDBStore(t.TempDir() + "/test.db")
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()

	err = store.SetValidated(ctx, "obsidian.apiKey", "sk-live-000")
	require.ErrorIs(t, err, ErrNoMasterKey)

	all, listErr := store.List(ctx)
	require.NoError(t, listErr)
	_, exists := all["obsidian.apiKey"]
	assert.False(t, exists, "a refused write must not persist any row")
}

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it. cmd_settings.go's set command prints via
// fmt.Printf straight to os.Stdout (not cmd.OutOrStdout()), so this is the
// only way to observe it end to end.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}

// TestSettingsSetCmd_MasksSecretValueInOutput guards the same defect class as
// the PATCH-response mask (settings/handler_test.go), on the CLI's own
// surface: "dashboard settings set obsidian.apiKey <value>" must not echo
// the plaintext it was just given into stdout, where it lands in terminal
// scrollback, tmux capture-pane, or a redirected log file.
func TestSettingsSetCmd_MasksSecretValueInOutput(t *testing.T) {
	isolateMasterKeyPaths(t)
	dbPath := t.TempDir() + "/test.db"

	out := captureStdout(t, func() {
		cmd := newSettingsCmd()
		cmd.SetArgs([]string{"set", "obsidian.apiKey", "sk-live-777", "--db", dbPath})
		require.NoError(t, cmd.Execute())
	})

	assert.NotContains(t, out, "sk-live-777")
	assert.Contains(t, out, secretbox.MaskedSentinel)
}
