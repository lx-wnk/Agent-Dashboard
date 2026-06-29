package parser

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
)

// Message is the decoded shape of one entry line from a Claude session JSONL file.
// ScanMessages yields one Message per successfully decoded line; malformed lines
// are silently skipped.
type Message struct {
	Type      string
	Subtype   string
	Timestamp time.Time
	Role      string
	Model     string
	Usage     *sdk.TokenUsage
	Content   json.RawMessage
}

// ScanMessages opens path, reads up to maxBytes (0 = whole file), decodes each
// JSONL line into a Message, and calls fn per decoded line. Malformed lines are
// silently skipped. ErrStopScan stops iteration without error; any other fn error
// propagates. Message.Content aliases the scanner's line buffer — copy it if it
// must outlive the fn call.
func ScanMessages(path string, maxBytes int64, fn func(m Message) error) error {
	rc, err := OpenJSONLReader(path, maxBytes)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer rc.Close() //nolint:errcheck

	return ScanJSONLLines(rc, func(line []byte) error {
		var outer struct {
			Type      string          `json:"type"`
			Subtype   string          `json:"subtype"`
			Timestamp string          `json:"timestamp"`
			Message   json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(line, &outer); err != nil {
			return nil // skip malformed outer envelope
		}
		var inner struct {
			Role    string          `json:"role"`
			Model   string          `json:"model"`
			Usage   *usageCounters  `json:"usage"`
			Content json.RawMessage `json:"content"`
		}
		// inner decode failure is non-fatal: some line types have no message field
		_ = json.Unmarshal(outer.Message, &inner)

		var ts time.Time
		if outer.Timestamp != "" {
			if parsed, perr := time.Parse(time.RFC3339Nano, outer.Timestamp); perr == nil {
				ts = parsed
			}
		}

		var usage *sdk.TokenUsage
		if inner.Usage != nil {
			usage = &sdk.TokenUsage{
				InputTokens:         inner.Usage.InputTokens,
				OutputTokens:        inner.Usage.OutputTokens,
				CacheCreationTokens: inner.Usage.CacheCreationTokens,
				CacheReadTokens:     inner.Usage.CacheReadTokens,
			}
		}

		return fn(Message{
			Type:      outer.Type,
			Subtype:   outer.Subtype,
			Timestamp: ts,
			Role:      inner.Role,
			Model:     inner.Model,
			Usage:     usage,
			Content:   inner.Content,
		})
	})
}
