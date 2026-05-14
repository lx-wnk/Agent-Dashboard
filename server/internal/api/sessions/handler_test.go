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

	// Accept any valid HTTP status code (200 or 500) — not a panic.
	assert.GreaterOrEqual(t, rec.Code, 200)
	assert.Less(t, rec.Code, 600)
}

// TestSessionsTimeline_InvalidIDReturnsBadRequest verifies that a malformed
// sessionId returns 400 before any filesystem access.
func TestSessionsTimeline_InvalidIDReturnsBadRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/not-a-uuid/timeline", nil)
	rec := httptest.NewRecorder()

	sessions.Timeline(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
