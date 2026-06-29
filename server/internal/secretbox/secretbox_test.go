package secretbox

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBox_EncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32) // all-zero key is fine for the round-trip test
	box, err := New(key)
	require.NoError(t, err)

	ct, nonce, err := box.Encrypt("super-secret-api-key")
	require.NoError(t, err)
	assert.NotEmpty(t, ct)
	assert.NotEmpty(t, nonce)
	assert.NotContains(t, ct, "super-secret") // ciphertext is opaque

	pt, err := box.Decrypt(ct, nonce)
	require.NoError(t, err)
	assert.Equal(t, "super-secret-api-key", pt)
}

func TestBox_WrongKeyFails(t *testing.T) {
	a, _ := New(make([]byte, 32))
	bKey := make([]byte, 32)
	bKey[0] = 1
	b, _ := New(bKey)
	ct, nonce, _ := a.Encrypt("x")
	_, err := b.Decrypt(ct, nonce)
	require.Error(t, err)
}

func TestNew_RejectsBadKeyLen(t *testing.T) {
	_, err := New(make([]byte, 16))
	require.Error(t, err)
}

func TestLoadOrGenerateMasterKey_InvalidKeyFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "dashboard-secret.key")
	require.NoError(t, os.WriteFile(keyPath, []byte("not-a-valid-hex-key\n"), 0o600))

	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	_, err := LoadOrGenerateMasterKey("")
	require.Error(t, err)

	// file must not have been overwritten
	content, readErr := os.ReadFile(keyPath)
	require.NoError(t, readErr)
	assert.Equal(t, "not-a-valid-hex-key\n", string(content))
}

func TestLoadOrGenerateMasterKey_GeneratesPersistsAndReuses(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	// Isolate HOME so a real ~/.claude key on this machine can't trigger the
	// legacy fallback and short-circuit generation.
	t.Setenv("HOME", t.TempDir())

	// First call: no key file exists — must generate and persist.
	key1, err := LoadOrGenerateMasterKey("")
	require.NoError(t, err)
	require.Len(t, key1, 32, "master key must be 32 bytes")

	// Key file must be persisted with owner-only permissions.
	keyPath := filepath.Join(dir, secretKeyFileName)
	info, err := os.Stat(keyPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "key file must be 0600")

	// Second call: must read and return the same key (idempotent bootstrap).
	key2, err := LoadOrGenerateMasterKey("")
	require.NoError(t, err)
	require.Equal(t, key1, key2, "second call must return the same persisted key")
}

func TestLoadOrGenerateMasterKey_LegacyFallback(t *testing.T) {
	// Redirect HOME so UserHomeDir() resolves to a controlled temp directory.
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create a valid key at the legacy ~/.claude path.
	legacyDir := filepath.Join(home, ".claude")
	require.NoError(t, os.MkdirAll(legacyDir, 0o700))
	const legacyKeyHex = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	wantKey, err := hex.DecodeString(legacyKeyHex)
	require.NoError(t, err)
	legacyPath := filepath.Join(legacyDir, secretKeyFileName)
	require.NoError(t, os.WriteFile(legacyPath, []byte(legacyKeyHex+"\n"), 0o600))

	// CLAUDE_CONFIG_DIR points to a different, empty directory.
	newDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", newDir)

	got, err := LoadOrGenerateMasterKey("")
	require.NoError(t, err)
	require.Equal(t, wantKey, got, "must return legacy ~/.claude key, not generate a new one")

	// No new key file must appear at the configured path.
	_, statErr := os.Stat(filepath.Join(newDir, secretKeyFileName))
	require.True(t, os.IsNotExist(statErr), "must not generate a new key when legacy key exists")
}
