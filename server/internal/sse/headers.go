package sse

import "net/http"

// WriteHeaders sets the canonical set of HTTP response headers for a
// Server-Sent-Events stream. It MUST be called before the first write or
// flush on w.
//
// Beyond the transport headers (Content-Type, Cache-Control, Connection,
// X-Accel-Buffering) it explicitly sets X-Content-Type-Options: nosniff.
// While the global SecurityHeaders middleware also sets nosniff, setting it
// here keeps the SSE stream protected against MIME-sniffing independently of
// middleware ordering — a defence-in-depth SSOT for every SSE endpoint.
func WriteHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
}
