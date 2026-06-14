// Package httputil holds small, dependency-free HTTP helpers shared across the
// server and the separately-built channel bridge binary. Stdlib-only by design.
package httputil

// Is2xx reports whether an HTTP status code is in the 2xx success range.
func Is2xx(code int) bool {
	return code >= 200 && code < 300
}
