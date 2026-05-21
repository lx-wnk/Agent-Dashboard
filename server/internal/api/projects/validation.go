// Package projects implements CRUD endpoints for Projects and ProjectFolders.
package projects

import (
	"regexp"
	"strings"
)

// SlugRE mirrors SLUG_RE in src/utils/validation.ts: lowercase alnum + hyphen,
// must start with alnum, total length 1..64.
var SlugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ColorRE matches #rgb or #rrggbb hex colors (case-insensitive).
var ColorRE = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// ValidateSlug returns true when s is a valid slug.
func ValidateSlug(s string) bool {
	return SlugRE.MatchString(s)
}

// ValidateColor returns true when c is a valid 3- or 6-digit hex color.
func ValidateColor(c string) bool {
	return ColorRE.MatchString(c)
}

// ValidateAbsolutePath returns true when p is an absolute POSIX path with no
// ".." segment. Empty paths are rejected. Trailing slashes are tolerated.
func ValidateAbsolutePath(p string) bool {
	if p == "" {
		return false
	}
	if !strings.HasPrefix(p, "/") {
		return false
	}
	// Reject any ".." segment (handles "..", "/..", "/foo/..", "/foo/../bar").
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}
