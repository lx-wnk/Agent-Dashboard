package auth_test

import (
	"encoding/base64"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/stretchr/testify/require"
)

func TestSignAndVerifyJWT(t *testing.T) {
	secret := "test-secret-32chars-long-minimum!"
	payload := auth.JWTPayload{Sub: "12345", Login: "testuser", IsAdmin: false}

	token, err := auth.SignJWT(payload, secret, 3600)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	got, err := auth.VerifyJWT(token, secret)
	require.NoError(t, err)
	require.Equal(t, "12345", got.Sub)
	require.Equal(t, "testuser", got.Login)
}

func TestVerifyJWT_Expired(t *testing.T) {
	secret := "test-secret-32chars-long-minimum!"
	payload := auth.JWTPayload{Sub: "12345", Login: "testuser"}

	token, err := auth.SignJWT(payload, secret, -1) // expired 1 second ago
	require.NoError(t, err)

	_, err = auth.VerifyJWT(token, secret)
	require.ErrorIs(t, err, auth.ErrTokenExpired)
}

func TestVerifyJWT_WrongSecret(t *testing.T) {
	token, err := auth.SignJWT(auth.JWTPayload{Sub: "x", Login: "x"}, "secret1", 3600)
	require.NoError(t, err)

	_, err = auth.VerifyJWT(token, "secret2")
	require.ErrorIs(t, err, auth.ErrTokenInvalid)
}

func TestVerifyJWT_MalformedToken(t *testing.T) {
	_, err := auth.VerifyJWT("not.a.valid.jwt.token.at.all", "secret")
	require.ErrorIs(t, err, auth.ErrTokenInvalid)
}

func TestVerifyJWT_InvalidHeader(t *testing.T) {
	// Header: {"alg":"RS256","typ":"JWT"} — valid structure but wrong alg
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"x","exp":9999999999}`))
	token := header + "." + body + ".fakesig"
	_, err := auth.VerifyJWT(token, "secret")
	require.ErrorIs(t, err, auth.ErrTokenInvalid)
}
