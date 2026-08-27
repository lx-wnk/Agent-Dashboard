// Package memory holds the system-owned memory store: spaces, entries, and
// what gets written into them.
package memory

import (
	"errors"

	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
	"github.com/lx-wnk/agent-dashboard/server/internal/sanitize"
)

// ErrEmptyAfterSanitize is returned when sanitizing summary or content leaves
// either one empty. A silently emptied entry is worse than a rejected write:
// nothing would otherwise surface that anything was lost.
var ErrEmptyAfterSanitize = errors.New("memory: entry emptied by sanitization")

// SanitizeForStore prepares a memory entry's summary and content for
// persistent storage.
//
// The parser's secret scrubber runs on read-out, right before API exposure.
// A memory entry is persistent, so a secret written into it would outlive
// every later scrub — this boundary has to be the write, not the read.
//
// Trojan-source control and bidi-override runes are stripped too: memory is
// a third boundary rendered to a human (the UI) and concatenated into
// prompts, the same two reasons the sanitize package already exists for the
// tool-call boundaries it covers. Sanitizing runs before scrubbing so an
// invisible control rune spliced into the middle of a secret cannot split it
// out of the scrubber's patterns.
func SanitizeForStore(summary, content string) (string, string, error) {
	cleanSummary := parser.ScrubSecrets(sanitize.ForDisplay(summary))
	cleanContent := parser.ScrubSecrets(sanitize.ForDisplay(content))

	if cleanSummary == "" || cleanContent == "" {
		return "", "", ErrEmptyAfterSanitize
	}
	return cleanSummary, cleanContent, nil
}
