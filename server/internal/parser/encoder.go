// Package parser reads Claude Code JSONL session logs and extracts agent data.
package parser

import "strings"

// EncodePath converts an absolute path to Claude Code's directory-encoding scheme.
// Claude replaces /, ., and _ all with - when naming project directories.
func EncodePath(absPath string) string {
	return strings.NewReplacer("/", "-", ".", "-", "_", "-").Replace(absPath)
}
