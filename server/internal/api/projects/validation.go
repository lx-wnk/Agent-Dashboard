// Package projects implements CRUD endpoints for Projects and ProjectFolders.
package projects

import (
	"regexp"
	"strings"

	"github.com/lx-wnk/agent-dashboard/server/internal/validation"
)

// ColorRE matches #rgb or #rrggbb hex colors (case-insensitive).
var ColorRE = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// ValidateSlug returns true when s is a valid slug.
// Delegates to the canonical validation package — do not define a local copy.
func ValidateSlug(s string) bool {
	return validation.IsValidSlug(s)
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
