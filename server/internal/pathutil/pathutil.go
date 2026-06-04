// Package pathutil provides filesystem path helpers shared across packages.
package pathutil

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandLeadingTilde replaces a leading `~` (bare or `~/`) with the user's home
// directory, mirroring shell tilde expansion. Other values pass through unchanged.
func ExpandLeadingTilde(v string) string {
	if v != "~" && !strings.HasPrefix(v, "~/") {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return v
	}
	if v == "~" {
		return home
	}
	return filepath.Join(home, v[2:])
}
