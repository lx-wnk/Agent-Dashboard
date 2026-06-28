package secretbox

import (
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
