package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}

type contextKey string

const payloadKey contextKey = "jwt_payload"

// RequireAuth is a chi middleware that validates the JWT from cookie or
// Authorization header. Unauthenticated requests receive 401.
func RequireAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				writeUnauthorized(w, "unauthorized")
				return
			}
			payload, err := VerifyJWT(token, secret)
			if err != nil {
				if errors.Is(err, ErrTokenExpired) {
					writeUnauthorized(w, "token expired")
					return
				}
				writeUnauthorized(w, "unauthorized")
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

// RequireAdmin is a chi middleware that rejects requests from non-admin users.
// It must be used inside a RequireAuth group.
//
// Stale-privilege detection: if the JWT claims IsAdmin but AdminGrantedAt is
// older than adminPrivilegeTTL (1 hour), the request is rejected with 403 and
// the client is instructed to re-login. This bounds the window in which a
// revoked admin still holds effective access even while their token is valid.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, ok := PayloadFromContext(r.Context())
		if !ok || !payload.IsAdmin {
			writeForbidden(w, "forbidden")
			return
		}
		// Enforce stale-privilege TTL when AdminGrantedAt is present.
		// Clamp negative age (AdminGrantedAt in the future) to avoid the check
		// always passing when the clock skew makes the subtraction negative.
		if payload.AdminGrantedAt > 0 {
			age := time.Now().Unix() - payload.AdminGrantedAt
			if age < 0 || age > adminPrivilegeTTL {
				writeForbidden(w, "admin privilege expired — please log in again")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeForbidden(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
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
