package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

type contextKey string

const payloadKey contextKey = "jwt_payload"

// RequireAuth is a chi middleware that validates the JWT from cookie or
// Authorization header. Unauthenticated requests receive 401.
func RequireAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			payload, err := VerifyJWT(token, secret)
			if err != nil {
				if errors.Is(err, ErrTokenExpired) {
					http.Error(w, `{"error":"token expired"}`, http.StatusUnauthorized)
					return
				}
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), payloadKey, payload)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// PayloadFromContext retrieves the JWT payload stored by RequireAuth.
func PayloadFromContext(ctx context.Context) (JWTPayload, bool) {
	p, ok := ctx.Value(payloadKey).(JWTPayload)
	return p, ok
}

func extractToken(r *http.Request) string {
	// Cookie first (web app uses httpOnly cookie)
	if c, err := r.Cookie("auth_token"); err == nil {
		return c.Value
	}
	// Fallback: Bearer header (API clients, MCP)
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}
