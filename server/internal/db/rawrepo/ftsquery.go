package rawrepo

import "strings"

// SanitizeFTSQuery converts a raw query string into a safe FTS5 MATCH expression.
// Each whitespace-separated token is wrapped in double quotes (internal quotes doubled)
// and given a prefix-match suffix (*).
//
// Example: `hello world` → `"hello"* "world"*`
func SanitizeFTSQuery(raw string) string {
	tokens := strings.Fields(raw)
	if len(tokens) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		escaped := strings.ReplaceAll(tok, `"`, `""`)
		parts = append(parts, `"`+escaped+`"*`)
	}
	return strings.Join(parts, " ")
}
