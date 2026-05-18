package auth_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/stretchr/testify/require"
)

// computeTestHMAC replicates the internal HMAC used by the auth package so that
// tampered-token tests can produce a correctly-signed but claim-invalid token.
func computeTestHMAC(data, secret string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

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

	// Use a TTL well outside the 60-second leeway so the expiry is unambiguous.
	token, err := auth.SignJWT(payload, secret, -120) // expired 2 minutes ago
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

func TestVerifyJWT_WrongIssuer(t *testing.T) {
	// Construct a structurally valid token signed with the correct secret but wrong iss.
	secret := "test-secret-32chars-long-minimum!"
	payload := auth.JWTPayload{Sub: "12345", Login: "testuser", Iss: "other-service", Aud: "agent-dashboard"}
	token, err := auth.SignJWT(payload, secret, 3600)
	require.NoError(t, err)
	// SignJWT overwrites Iss, so craft the raw token manually to inject a wrong issuer.
	_ = token
	// Use a manually assembled payload to bypass SignJWT's normalization.
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	bod := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"12345","login":"testuser","isAdmin":false,"exp":9999999999,"iss":"evil","aud":"agent-dashboard"}`))
	mac := computeTestHMAC(hdr+"."+bod, secret)
	tampered := hdr + "." + bod + "." + base64.RawURLEncoding.EncodeToString(mac)
	_, err = auth.VerifyJWT(tampered, secret)
	require.ErrorIs(t, err, auth.ErrTokenInvalid)
}

func TestVerifyJWT_WrongAudience(t *testing.T) {
	secret := "test-secret-32chars-long-minimum!"
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	bod := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"12345","login":"testuser","isAdmin":false,"exp":9999999999,"iss":"agent-dashboard","aud":"other-audience"}`))
	mac := computeTestHMAC(hdr+"."+bod, secret)
	tampered := hdr + "." + bod + "." + base64.RawURLEncoding.EncodeToString(mac)
	_, err := auth.VerifyJWT(tampered, secret)
	require.ErrorIs(t, err, auth.ErrTokenInvalid)
}
