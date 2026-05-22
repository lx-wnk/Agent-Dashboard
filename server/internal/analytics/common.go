// Package analytics — shared types and constants used by ngrams and
// visualizations. Single source of truth (SSOT) for things that would
// otherwise duplicate across files.
package analytics

import (
	"regexp"
	"time"
)

// ToolNameRE constrains the names we accept from tool_use blocks in JSONL
// session logs. The same regex gates both pattern ngram discovery
// (ngrams.go) and the visualization data-shaping helpers
// (visualizations.go) so the two paths agree on what counts as a tool.
//
//nolint:revive // exported for cross-file reuse within the analytics package
var ToolNameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// ScanOpts narrows the JSONL scan that the visualization Build* helpers
// share via scanSessionsForTools.
type ScanOpts struct {
	// Sessions is an explicit allow-list of session IDs (UUIDs). If empty,
	// every JSONL file under the configured Claude config dirs is eligible
	// and the most-recent MaxSessions are kept.
	Sessions []string
	// From and To are inclusive bounds on session-file mtime / message
	// timestamps. Zero values mean unbounded on that side.
	From time.Time
	To   time.Time
	// MaxSessions caps how many sessions a single scan touches.
	// A zero value falls back to DefaultMaxSessions.
	MaxSessions int
}

// DefaultMaxSessions is the hard cap on aggregate visualization scans.
const DefaultMaxSessions = 20

// ToolCall is one tool_use event observed in a JSONL session log.
type ToolCall struct {
	SessionID string
	Name      string
	// ID is the tool_use_id where present; empty otherwise.
	ID string
	// Timestamp is parsed from the JSONL message envelope; zero if absent
	// or unparseable.
	Timestamp time.Time
}
