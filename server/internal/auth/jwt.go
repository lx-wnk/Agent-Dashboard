// Package auth provides JWT authentication utilities and HTTP middleware.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Sentinel errors for JWT verification failures.
var (
	ErrTokenInvalid = errors.New("token invalid")
	ErrTokenExpired = errors.New("token expired")
)

// JWTPayload is the JWT body — matches the TypeScript JwtPayload interface.
type JWTPayload struct {
	Sub            string `json:"sub"`   // GitHub numeric user ID
	Login          string `json:"login"` // GitHub username
	IsAdmin        bool   `json:"isAdmin"`
	AdminGrantedAt int64  `json:"aga,omitempty"` // Unix timestamp when admin was granted; set iff IsAdmin==true
	Exp            int64  `json:"exp"`           // Unix timestamp
	Iat            int64  `json:"iat,omitempty"` // Issued-at Unix timestamp
	Iss            string `json:"iss,omitempty"` // Issuer
	Aud            string `json:"aud,omitempty"` // Audience
}

// adminPrivilegeTTL is the maximum age of the AdminGrantedAt claim before an
// admin-gated endpoint must force re-login. This bounds the stale-privilege
// window to 1 hour even when the JWT itself is still valid.
const adminPrivilegeTTL = int64(3600)

// OAuthStatePayload is the payload for short-lived OAuth state tokens.
// Kept separate from JWTPayload to prevent state tokens from being accepted
// where session tokens are expected.
type OAuthStatePayload struct {
	Sub string `json:"sub"` // always "oauth-state"
	Exp int64  `json:"exp"`
	Aud string `json:"aud"` // always "agent-dashboard:oauth-state"
}

func base64url(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func computeHMAC(data, secret string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

const jwtIssuer = "agent-dashboard"
const jwtAudience = "agent-dashboard"

// SignJWT creates an HS256 JWT token. expiresInSeconds is added to now().
func SignJWT(payload JWTPayload, secret string, expiresInSeconds int64) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", fmt.Errorf("marshal header: %w", err)
	}
	now := time.Now().Unix()
	payload.Exp = now + expiresInSeconds
	payload.Iat = now
	payload.Iss = jwtIssuer
	payload.Aud = jwtAudience
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	h := base64url(header)
	b := base64url(body)
	sig := base64url(computeHMAC(h+"."+b, secret))
	return h + "." + b + "." + sig, nil
}

// VerifyJWT validates an HS256 JWT and returns the payload.
// Returns ErrTokenInvalid for structural/signature errors, ErrTokenExpired for expired tokens.
// Rejects tokens with sub == "oauth-state" as defense-in-depth against state token reuse.
func VerifyJWT(token, secret string) (JWTPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return JWTPayload{}, ErrTokenInvalid
	}
	h, b, sig := parts[0], parts[1], parts[2]

	// Verify header
	headerBytes, err := base64.RawURLEncoding.DecodeString(h)
	if err != nil {
		return JWTPayload{}, ErrTokenInvalid
	}
	var header map[string]string
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return JWTPayload{}, ErrTokenInvalid
	}
	if header["alg"] != "HS256" || header["typ"] != "JWT" {
		return JWTPayload{}, ErrTokenInvalid
	}

	// Verify signature — timing-safe comparison on raw bytes
	sigBytes, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return JWTPayload{}, ErrTokenInvalid
	}
	expected := computeHMAC(h+"."+b, secret)
	if !hmac.Equal(expected, sigBytes) {
		return JWTPayload{}, ErrTokenInvalid
	}

	// Decode payload
	bodyBytes, err := base64.RawURLEncoding.DecodeString(b)
	if err != nil {
		return JWTPayload{}, ErrTokenInvalid
	}
	var payload JWTPayload
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return JWTPayload{}, ErrTokenInvalid
	}
	// Defense-in-depth: reject OAuth state tokens used as session tokens.
	if payload.Sub == "oauth-state" {
		return JWTPayload{}, ErrTokenInvalid
	}
	if payload.Iss != jwtIssuer {
		return JWTPayload{}, ErrTokenInvalid
	}
	if payload.Aud != jwtAudience {
		return JWTPayload{}, ErrTokenInvalid
	}
	if time.Now().Unix() > payload.Exp {
		return JWTPayload{}, ErrTokenExpired
	}
	// Reject tokens issued more than 60s in the future (clock skew guard).
	if payload.Iat > 0 && payload.Iat > time.Now().Unix()+60 {
		return JWTPayload{}, ErrTokenInvalid
	}
	return payload, nil
}

// SignOAuthState creates a short-lived HS256 JWT for use as the OAuth CSRF state parameter.
func SignOAuthState(secret string) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", fmt.Errorf("marshal header: %w", err)
	}
	payload := OAuthStatePayload{
		Sub: "oauth-state",
		Aud: "agent-dashboard:oauth-state",
		Exp: time.Now().Unix() + 300,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	h := base64url(header)
	b := base64url(body)
	sig := base64url(computeHMAC(h+"."+b, secret))
	return h + "." + b + "." + sig, nil
}

// VerifyOAuthState validates an OAuth state token and returns its payload.
// Rejects tokens with wrong audience or subject.
func VerifyOAuthState(token, secret string) (OAuthStatePayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return OAuthStatePayload{}, ErrTokenInvalid
	}
	h, b, sig := parts[0], parts[1], parts[2]

	// Verify header
	headerBytes, err := base64.RawURLEncoding.DecodeString(h)
	if err != nil {
		return OAuthStatePayload{}, ErrTokenInvalid
	}
	var header map[string]string
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return OAuthStatePayload{}, ErrTokenInvalid
	}
	if header["alg"] != "HS256" || header["typ"] != "JWT" {
		return OAuthStatePayload{}, ErrTokenInvalid
	}

	// Verify signature — timing-safe comparison on raw bytes
	sigBytes, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return OAuthStatePayload{}, ErrTokenInvalid
	}
	expected := computeHMAC(h+"."+b, secret)
	if !hmac.Equal(expected, sigBytes) {
		return OAuthStatePayload{}, ErrTokenInvalid
	}

	// Decode payload
	bodyBytes, err := base64.RawURLEncoding.DecodeString(b)
	if err != nil {
		return OAuthStatePayload{}, ErrTokenInvalid
	}
	var payload OAuthStatePayload
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return OAuthStatePayload{}, ErrTokenInvalid
	}
	if payload.Aud != "agent-dashboard:oauth-state" {
		return OAuthStatePayload{}, ErrTokenInvalid
	}
	if payload.Sub != "oauth-state" {
		return OAuthStatePayload{}, ErrTokenInvalid
	}
	if time.Now().Unix() > payload.Exp {
		return OAuthStatePayload{}, ErrTokenExpired
	}
	return payload, nil
}
