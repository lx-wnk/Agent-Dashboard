// Package auth provides JWT authentication utilities and HTTP middleware.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
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

// jwtClaims embeds RegisteredClaims and carries our custom fields.
type jwtClaims struct {
	jwt.RegisteredClaims
	Login          string `json:"login"`
	IsAdmin        bool   `json:"isAdmin"`
	AdminGrantedAt int64  `json:"aga,omitempty"`
}

// oauthStateClaims is the claims type for short-lived OAuth state tokens.
type oauthStateClaims struct {
	jwt.RegisteredClaims
}

// OAuthStatePayload is the payload for short-lived OAuth state tokens.
// Kept separate from JWTPayload to prevent state tokens from being accepted
// where session tokens are expected.
type OAuthStatePayload struct {
	Sub string `json:"sub"` // always "oauth-state"
	Exp int64  `json:"exp"`
	Aud string `json:"aud"` // always "agent-dashboard:oauth-state"
}

const jwtIssuer = "agent-dashboard"
const jwtAudience = "agent-dashboard"

// SignJWT creates an HS256 JWT token. expiresInSeconds is added to now().
func SignJWT(payload JWTPayload, secret string, expiresInSeconds int64) (string, error) {
	now := time.Now()
	claims := jwtClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   payload.Sub,
			Issuer:    jwtIssuer,
			Audience:  jwt.ClaimStrings{jwtAudience},
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(expiresInSeconds) * time.Second)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
		Login:          payload.Login,
		IsAdmin:        payload.IsAdmin,
		AdminGrantedAt: payload.AdminGrantedAt,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign jwt: %w", err)
	}
	return signed, nil
}

// VerifyJWT validates an HS256 JWT and returns the payload.
// Returns ErrTokenInvalid for structural/signature errors, ErrTokenExpired for expired tokens.
// Rejects tokens with sub == "oauth-state" as defense-in-depth against state token reuse.
func VerifyJWT(tokenStr, secret string) (JWTPayload, error) {
	var claims jwtClaims
	token, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	},
		jwt.WithIssuedAt(),
		jwt.WithIssuer(jwtIssuer),
		jwt.WithAudience(jwtAudience),
		jwt.WithLeeway(60*time.Second),
	)

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return JWTPayload{}, ErrTokenExpired
		}
		return JWTPayload{}, ErrTokenInvalid
	}
	if !token.Valid {
		return JWTPayload{}, ErrTokenInvalid
	}

	// Defense-in-depth: reject OAuth state tokens used as session tokens.
	if claims.Subject == "oauth-state" {
		return JWTPayload{}, ErrTokenInvalid
	}

	exp := int64(0)
	if claims.ExpiresAt != nil {
		exp = claims.ExpiresAt.Unix()
	}
	iat := int64(0)
	if claims.IssuedAt != nil {
		iat = claims.IssuedAt.Unix()
	}
	aud := ""
	if len(claims.Audience) > 0 {
		aud = claims.Audience[0]
	}

	return JWTPayload{
		Sub:            claims.Subject,
		Login:          claims.Login,
		IsAdmin:        claims.IsAdmin,
		AdminGrantedAt: claims.AdminGrantedAt,
		Exp:            exp,
		Iat:            iat,
		Iss:            claims.Issuer,
		Aud:            aud,
	}, nil
}

// SignOAuthState creates a short-lived HS256 JWT for use as the OAuth CSRF state parameter.
func SignOAuthState(secret string) (string, error) {
	claims := oauthStateClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "oauth-state",
			Audience:  jwt.ClaimStrings{"agent-dashboard:oauth-state"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign oauth state: %w", err)
	}
	return signed, nil
}

// VerifyOAuthState validates an OAuth state token and returns its payload.
// Rejects tokens with wrong audience or subject.
func VerifyOAuthState(tokenStr, secret string) (OAuthStatePayload, error) {
	var claims oauthStateClaims
	token, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	},
		jwt.WithAudience("agent-dashboard:oauth-state"),
	)

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return OAuthStatePayload{}, ErrTokenExpired
		}
		return OAuthStatePayload{}, ErrTokenInvalid
	}
	if !token.Valid {
		return OAuthStatePayload{}, ErrTokenInvalid
	}
	if claims.Subject != "oauth-state" {
		return OAuthStatePayload{}, ErrTokenInvalid
	}

	exp := int64(0)
	if claims.ExpiresAt != nil {
		exp = claims.ExpiresAt.Unix()
	}
	aud := ""
	if len(claims.Audience) > 0 {
		aud = claims.Audience[0]
	}
	return OAuthStatePayload{
		Sub: claims.Subject,
		Exp: exp,
		Aud: aud,
	}, nil
}
