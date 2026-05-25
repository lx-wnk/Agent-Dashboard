// Package validation provides shared input-validation constants and helpers.
package validation

import "regexp"

// SlugRE matches valid task slugs: starts with a lowercase alphanumeric character,
// followed by up to 63 lowercase alphanumeric or hyphen characters (64 chars max total).
// Canonical pattern — mirrors src/utils/validation.ts SLUG_RE. Import this instead of
// defining a local copy.
var SlugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// SlugPatternMessage is the human-readable description of SlugRE for error messages.
const SlugPatternMessage = "slug must match [a-z0-9][a-z0-9-]{0,63}"

// IsValidSlug reports whether s matches SlugRE.
// The regex enforces the 64-character cap via {0,63}, so no separate length check is needed.
func IsValidSlug(s string) bool {
	return SlugRE.MatchString(s)
}
