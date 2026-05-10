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
	Sub     string `json:"sub"`   // GitHub numeric user ID
	Login   string `json:"login"` // GitHub username
	IsAdmin bool   `json:"isAdmin"`
	Exp     int64  `json:"exp"` // Unix timestamp
}

func base64url(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func computeHMAC(data, secret string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

// SignJWT creates an HS256 JWT token. expiresInSeconds is added to now().
func SignJWT(payload JWTPayload, secret string, expiresInSeconds int64) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", fmt.Errorf("marshal header: %w", err)
	}
	payload.Exp = time.Now().Unix() + expiresInSeconds
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
	if time.Now().Unix() > payload.Exp {
		return JWTPayload{}, ErrTokenExpired
	}
	return payload, nil
}
