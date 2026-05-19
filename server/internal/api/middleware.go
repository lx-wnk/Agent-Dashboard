package api

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// debugPaths contains path prefixes that should be logged at Debug level
// to avoid noise from high-frequency polling and long-lived SSE connections.
var debugPaths = []string{
	"/api/hooks/pending",
	"/api/system/health",
	"/api/system",
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

// RequireSameOriginForMutations blocks cross-origin mutation requests.
// It checks the Origin or Referer header for non-GET/HEAD/OPTIONS requests
// and rejects any that come from a different origin than the server itself.
// This defends against drive-by CSRF in the local loopback trust model.
//
// Bearer-token requests (Authorization header present) are exempt: browsers
// cannot set arbitrary Authorization headers cross-origin without a CORS
// preflight, so they are CSRF-immune by definition.
func RequireSameOriginForMutations(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.Method
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		// Bearer-token clients (MCP agents, curl, Claude Code sessions) are exempt.
		if r.Header.Get("Authorization") != "" {
			next.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if origin == "" {
			// No Origin header — check Referer as fallback.
			origin = r.Header.Get("Referer")
		}
		// Fail-closed: if both Origin and Referer are absent for a mutating
		// request, deny it — there is no way to verify the request source.
		if origin == "" {
			http.Error(w, `{"error":"missing Origin header"}`, http.StatusForbidden)
			return
		}
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host != r.Host {
			http.Error(w, `{"error":"cross-origin request denied"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
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
		// style-src 'self' (no 'unsafe-inline'): the production Vite build extracts all
		// SFC styles to linked .css files — no <style> tags are injected at runtime.
		// If a third-party library that injects inline styles is added in the future,
		// prefer SHA-256 hashes over re-adding 'unsafe-inline'.
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: https://avatars.githubusercontent.com; connect-src 'self'; font-src 'self' data:; object-src 'none'; worker-src 'self'; manifest-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}
