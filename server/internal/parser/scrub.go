package parser

import "regexp"

// secretPatterns is the list of regexes used to redact secrets from session
// content before it is surfaced in API responses.
var secretPatterns = []*regexp.Regexp{
	// Common key/token/secret/password assignment patterns (case-insensitive).
	regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|passwd|bearer|authorization)[^\n]{0,5}[=:]\s*\S+`),
	// OpenAI-style secret keys (sk-…).
	regexp.MustCompile(`sk-[a-zA-Z0-9]{32,}`),
	// Anthropic API keys (sk-ant-…).
	regexp.MustCompile(`sk-ant-[a-zA-Z0-9_\-]{20,}`),
	// GitHub personal access tokens (ghp_…).
	regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),
	// Other GitHub token types (gho_, ghs_, ghu_, ghr_, ghp_).
	regexp.MustCompile(`gh[ospru]_[a-zA-Z0-9]{36}`),
	// GitLab personal access tokens.
	regexp.MustCompile(`glpat-[a-zA-Z0-9_\-]{20}`),
	// Slack tokens.
	regexp.MustCompile(`xox[baprs]-[a-zA-Z0-9\-]+`),
	// AWS access key IDs.
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	// PEM/private key blocks.
	regexp.MustCompile(`-----BEGIN [A-Z ]+PRIVATE KEY-----[\s\S]+?-----END [A-Z ]+PRIVATE KEY-----`),
	// JWT tokens (3-part base64url structure, eyJ prefix indicates JSON header).
	regexp.MustCompile(`eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`),
	// Conservative base64 blobs — only very long sequences to avoid false positives.
	regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`),
}

// scrubSecrets replaces known secret patterns in s with "[REDACTED]".
// It is intentionally conservative: the base64 pattern requires 40+ characters
// to avoid false positives on legitimate short encoded values.
func scrubSecrets(s string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}
	return s
}

// ScrubSecrets is the exported entry point onto scrubSecrets for callers
// outside this package (e.g. memory, sanitizing content before it is stored).
// secretPatterns itself stays unexported: a caller that could reach the slice
// could mutate the source of truth every other caller relies on.
func ScrubSecrets(s string) string {
	return scrubSecrets(s)
}
