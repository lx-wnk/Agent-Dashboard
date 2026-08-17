package sse

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteHeadersSetsTheCanonicalSSESet(t *testing.T) {
	rec := httptest.NewRecorder()

	WriteHeaders(rec)

	h := rec.Header()
	require.Equal(t, "text/event-stream; charset=utf-8", h.Get("Content-Type"))
	require.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
	require.Equal(t, "no-cache, no-transform", h.Get("Cache-Control"))
	require.Equal(t, "keep-alive", h.Get("Connection"))
	require.Equal(t, "no", h.Get("X-Accel-Buffering"))
}

// The nosniff header is deliberately duplicated here and in the SecurityHeaders
// middleware, so an SSE stream stays protected whatever the middleware order is.
func TestWriteHeadersSetsNosniffWithoutMiddleware(t *testing.T) {
	rec := httptest.NewRecorder()

	WriteHeaders(rec)

	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
}

// no-transform stops a gzipping proxy or a CDN minifier from rewriting the byte
// stream, which would break SSE framing.
func TestWriteHeadersForbidsIntermediaryTransforms(t *testing.T) {
	rec := httptest.NewRecorder()

	WriteHeaders(rec)

	require.Contains(t, rec.Header().Get("Cache-Control"), "no-transform")
}
