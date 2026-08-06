package tools

import "time"

const isoFormat = "2006-01-02T15:04:05Z"

// tsFmt renders a timestamp in the wire format every MCP view uses. Shared by
// the read and write paths, so it lives outside both.
func tsFmt(t time.Time) string { return t.UTC().Format(isoFormat) }
