package validation

import "regexp"

// ColorRE matches #rgb or #rrggbb hex colors (case-insensitive).
// Canonical pattern — import this instead of defining a local copy.
var ColorRE = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// ColorPatternMessage is the human-readable description of ColorRE for error messages.
const ColorPatternMessage = "color must be #rgb or #rrggbb hex"

// IsValidColor reports whether c matches ColorRE.
func IsValidColor(c string) bool {
	return ColorRE.MatchString(c)
}
