package tools

import "time"

const isoFormat = "2006-01-02T15:04:05Z"

// tsFmt renders a timestamp in the wire format every MCP view uses.
func tsFmt(t time.Time) string { return t.UTC().Format(isoFormat) }
