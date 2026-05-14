package api

import (
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGzipResponseWriter_ImplementsFlusher verifies that gzipResponseWriter
// satisfies http.Flusher so SSE-adjacent handlers can flush through gzip.
func TestGzipResponseWriter_ImplementsFlusher(t *testing.T) {
	rec := httptest.NewRecorder()
	gz, err := gzip.NewWriterLevel(rec, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	w := &gzipResponseWriter{ResponseWriter: rec, Writer: gz}

	_, ok := any(w).(http.Flusher)
	if !ok {
		t.Fatal("gzipResponseWriter does not implement http.Flusher")
	}
}

// TestGzipMiddleware_SetsVaryHeader verifies Vary: Accept-Encoding is present
// on all responses — both when the client requests gzip and when it does not.
// Per RFC 7234 the header must be unconditional so shared caches never serve a
// stale uncompressed response to a gzip-accepting client.
func TestGzipMiddleware_SetsVaryHeader(t *testing.T) {
	handler := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name           string
		acceptEncoding string
	}{
		{name: "with gzip", acceptEncoding: "gzip"},
		{name: "without gzip", acceptEncoding: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.acceptEncoding != "" {
				req.Header.Set("Accept-Encoding", tc.acceptEncoding)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			vary := rec.Header().Get("Vary")
			if vary != "Accept-Encoding" {
				t.Errorf("expected Vary: Accept-Encoding, got %q", vary)
			}
		})
	}
}

// TestGzipMiddleware_FlushForwardsToInner verifies that Flush() on a
// gzipResponseWriter does not panic and propagates to the inner ResponseWriter.
func TestGzipMiddleware_FlushForwardsToInner(t *testing.T) {
	flushed := false
	inner := &fakeFlushWriter{ResponseRecorder: httptest.NewRecorder(), onFlush: func() { flushed = true }}

	gz, err := gzip.NewWriterLevel(inner, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	w := &gzipResponseWriter{ResponseWriter: inner, Writer: gz}

	flusher, ok := any(w).(http.Flusher)
	if !ok {
		t.Fatal("gzipResponseWriter does not implement http.Flusher")
	}
	flusher.Flush()

	if !flushed {
		t.Error("Flush() did not propagate to inner writer")
	}
}

type fakeFlushWriter struct {
	*httptest.ResponseRecorder
	onFlush func()
}

func (f *fakeFlushWriter) Flush() { f.onFlush() }
