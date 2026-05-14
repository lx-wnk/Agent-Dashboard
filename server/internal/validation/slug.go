// Package validation provides shared input-validation constants and helpers.
package validation

import "regexp"

// SlugRE matches valid task slugs: lowercase alphanumeric segments separated by
// hyphens, no leading or trailing hyphens, 1–64 characters total.
// Canonical pattern — import this instead of defining a local copy.
var SlugRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// SlugPatternMessage is the human-readable description of SlugRE for error messages.
const SlugPatternMessage = "slug must match ^[a-z0-9]+(?:-[a-z0-9]+)*$ and be at most 64 characters"

// IsValidSlug reports whether s matches SlugRE and does not exceed 64 characters.
func IsValidSlug(s string) bool {
	return len(s) <= 64 && SlugRE.MatchString(s)
}
