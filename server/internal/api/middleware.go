package api

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// debugPaths contains path prefixes that should be logged at Debug level
// to avoid noise from high-frequency polling and long-lived SSE connections.
var debugPaths = []string{
	"/api/hooks/pending",
	"/api/system/health",
	"/api/agents/stream",
	"/api/tasks/stream",
}

func isDebugPath(path string) bool {
	for _, prefix := range debugPaths {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// SlogMiddleware logs each request with method, path, status, and duration.
// High-frequency and background paths (health checks, SSE streams, polling)
// are logged at Debug level; everything else at Info.
func SlogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		args := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"duration", time.Since(start),
			"requestID", chimiddleware.GetReqID(r.Context()),
		}
		if isDebugPath(r.URL.Path) {
			slog.Debug("request", args...)
		} else {
			slog.Info("request", args...)
		}
	})
}

// SecurityHeaders sets security-relevant HTTP response headers.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Cross-Origin-Embedder-Policy", "require-corp")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https://avatars.githubusercontent.com; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}
