// Package domainerr holds the canonical domain error sentinels shared across
// layers. It imports only the standard library so any package — including the
// db layer — can depend on it without pulling in net/http or other transitive
// dependencies. The HTTP mapping of these sentinels lives in package apierr.
package domainerr

import "errors"

// Sentinel errors — use errors.Is() to check.
var (
	ErrNotFound   = errors.New("not found")
	ErrConflict   = errors.New("conflict")
	ErrBadRequest = errors.New("bad request")
	ErrForbidden  = errors.New("forbidden")
)
