// Package spawners implements admin-only CRUD endpoints for custom spawners.
package spawners

import (
	"os"
	"regexp"
	"strings"
)

// DefaultAllowedCommands are the bare command names always permitted in the
// spawners.command field. Extra entries can be appended via the
// DASHBOARD_SPAWNER_ALLOWED_COMMANDS env var (comma-separated; each entry is
// either a bare name or an absolute path prefix).
var DefaultAllowedCommands = []string{"claude", "claude-code", "npx"}

// blockedAbsolutePathPrefixes are forbidden roots for absolute-path commands —
// world-writable locations where a user could plant a binary.
var blockedAbsolutePathPrefixes = []string{"/tmp/", "/var/tmp/"}

// slugRE mirrors src/utils/validation.ts (lowercase alnum + hyphen, 1..64 chars).
var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// envKeyForbidden matches shell metacharacters and control chars that must
// never appear in an env key.
var envKeyForbidden = regexp.MustCompile("[;&|$`\n\r\t ]")

const envValueMaxLen = 4096

// ValidateSlug returns true when s is a valid slug.
func ValidateSlug(s string) bool {
	return slugRE.MatchString(s)
}

// ValidateCommand returns true when command is allowed per the bare-name list
// (DefaultAllowedCommands plus DASHBOARD_SPAWNER_ALLOWED_COMMANDS) or is an
// absolute path that is NOT under /tmp or /var/tmp.
func ValidateCommand(command string) bool {
	if command == "" {
		return false
	}
	allowedExtra := extraAllowedCommandsFromEnv()

	if strings.HasPrefix(command, "/") {
		// Absolute path: reject blocked prefixes.
		for _, bad := range blockedAbsolutePathPrefixes {
			if strings.HasPrefix(command, bad) {
				return false
			}
		}
		// Allow if matched by an env-provided absolute-path prefix.
		for _, extra := range allowedExtra {
			if strings.HasPrefix(extra, "/") && strings.HasPrefix(command, extra) {
				return true
			}
		}
		// Absolute paths outside blocked prefixes are allowed by default —
		// the operator chose the absolute form deliberately.
		return true
	}

	// Bare name — must appear in default list or extra-allowed list (as bare name).
	for _, name := range DefaultAllowedCommands {
		if command == name {
			return true
		}
	}
	for _, extra := range allowedExtra {
		if !strings.HasPrefix(extra, "/") && command == extra {
			return true
		}
	}
	return false
}

// ValidateEnv enforces:
//   - key non-empty
//   - key contains no shell metacharacters or whitespace/newlines
//   - value length <= 4096
func ValidateEnv(env map[string]string) (string, bool) {
	for k, v := range env {
		if k == "" {
			return "env key must be non-empty", false
		}
		if envKeyForbidden.MatchString(k) {
			return "env key contains forbidden characters", false
		}
		if len(v) > envValueMaxLen {
			return "env value exceeds 4096 chars", false
		}
	}
	return "", true
}

// extraAllowedCommandsFromEnv reads and parses
// DASHBOARD_SPAWNER_ALLOWED_COMMANDS into a slice (trims whitespace, drops
// empty entries).
func extraAllowedCommandsFromEnv() []string {
	raw := os.Getenv("DASHBOARD_SPAWNER_ALLOWED_COMMANDS")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
