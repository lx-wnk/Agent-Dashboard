// Package sessions_test provides smoke tests for the sessions HTTP handlers.
package sessions_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/sessions"
)

// TestSessionsList_DoesNotPanic is a smoke test that the List handler does not panic
// and returns a valid HTTP status code.
func TestSessionsList_DoesNotPanic(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()

	// The handler is a plain http.HandlerFunc — call it directly.
	sessions.List(rec, req)

	// Accept 200 (success, even with 0 sessions) or 500 (internal error from scanner).
	// Any other code indicates a handler bug or panic.
	assert.Contains(t, []int{http.StatusOK, http.StatusInternalServerError}, rec.Code)
}

// TestSessionsTimeline_InvalidIDReturnsBadRequest verifies that a malformed
// sessionId returns 400 before any filesystem access.
// Uses http.NewServeMux so that r.PathValue("sessionId") is populated correctly.
func TestSessionsTimeline_InvalidIDReturnsBadRequest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions/{sessionId}/timeline", sessions.Timeline)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/not-a-uuid/timeline", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
