// Package permissions defines the domain constants for the pipeline permission model.
package permissions

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

// safeBashCommands is the canonical allow-list of command names that may appear
// as the first token in a Bash permission pattern.  Any command not in this set
// is rejected regardless of arguments.  Names are lower-cased; comparison is
// case-insensitive after path-prefix stripping.
//
// Extend via explicit review only — a new interpreter or shell escape vector
// must be consciously accepted here, not accidentally permitted by a missed
// block-list entry.
var safeBashCommands = map[string]bool{
	"pnpm":    true,
	"npm":     true,
	"npx":     true,
	"yarn":    true,
	"git":     true,
	"task":    true,
	"go":      true,
	"cargo":   true,
	"python":  true,
	"python3": true,
	"pip":     true,
	"pip3":    true,
	"ls":      true,
	"cat":     true,
	"head":    true,
	"tail":    true,
	"grep":    true,
	"rg":      true,
	"find":    true,
	"mkdir":   true,
	"touch":   true,
	"cp":      true,
	"mv":      true,
	"echo":    true,
	"pwd":     true,
	"env":     true,
	"printenv": true,
	"which":   true,
	"type":    true,
	"wc":      true,
	"sort":    true,
	"uniq":    true,
	"diff":    true,
	"patch":   true,
	"make":    true,
	"rake":    true,
	"bundle":  true,
	"composer": true,
	"mvn":     true,
	"gradle":  true,
	"dotnet":  true,
	"rustup":  true,
	"node":    true,
	"deno":    true,
	"bun":     true,
	"tsc":     true,
	"eslint":  true,
	"prettier": true,
	"golangci-lint": true,
	"gofmt":   true,
	"govet":   true,
	"air":     true,
	"wire":    true,
	"protoc":  true,
	"docker":  true,
	"kubectl": true,
	"helm":    true,
	"terraform": true,
	"jq":      true,
	"yq":      true,
	"curl":    false, // explicitly false for documentation — never safe in this context
}

// shellInjectionRE matches shell constructs that are dangerous regardless of
// which command they appear in.  This is a secondary defence after the
// allow-list check.
var shellInjectionRE = regexp.MustCompile(
	`(?i)` +
		`\$\(` + // command substitution $(...)
		`|<\(` + // process substitution <(...)
		"|`" + // backtick substitution
		`|\beval\b` + // eval
		`|\\x[0-9a-fA-F]{2}` + // hex escape sequences
		`|\\u[0-9a-fA-F]{4}` + // unicode escape sequences
		`|\|\s*\w` + // pipe to another command
		`|&&\s*\w` + // AND-chained command
		`|;\s*\w`, // semicolon-chained command
)

// DangerousBashRE is retained for backward compatibility with existing callers
// (spawner.go, permission_request_routes.go).  New code should use
// IsSafeBashPattern instead, which provides allow-list semantics rather than
// block-list semantics.
//
// The regex matches patterns that must never appear in a Bash allow-list entry.
// It intentionally overlaps with IsSafeBashPattern — both must agree that a
// pattern is dangerous before it can be rejected.
var DangerousBashRE = regexp.MustCompile(
	`(?i)` +
		// Explicit dangerous commands not reachable via safeBashCommands.
		`\bsudo\b` +
		`|\bcurl\b` +
		`|\bwget\b` +
		`|\bnc\b|\bncat\b|\bnetcat\b` +
		`|bash\s+-c|sh\s+-c|dash\s+-c|zsh\s+-c` +
		`|\beval\b` +
		// Interpreters not in the safe list (pypy, perl, ruby, node -e, etc.).
		`|python[23]?(?:\.\d+)?\s+-c` +
		`|pypy\d*(?:\.\d+)?` +
		`|node(?:js)?\s+-e` +
		`|perl\d*\s+-e` +
		`|ruby\d*\s+-e` +
		`|deno\s+eval` +
		`|bun\s+eval` +
		// Encoding / decoding that could hide payloads.
		`|base64\s+-d` +
		`|xxd\s+-r` +
		// Shell-injection constructs.
		`|\$\(|<\(` + // command/process substitution
		"|`" + // backtick
		// Redirect to arbitrary paths (e.g. >file, >/tmp/x).
		`|>\s*[\w/]` +
		`|<\s*[\w/]` +
		// Destructive file operations.
		`|chmod\s+\+x` +
		`|rm\s+-rf` +
		`|exec\s+\w` +
		// Lateral-movement via xargs regardless of path prefix.
		`|(?:^|/)xargs\b` +
		// find -exec (arbitrary command execution).
		`|find\s+.*-exec` +
		// Semicolon / AND chains to a non-empty token.
		`|;\s*\w` +
		`|&&\s*\w` +
		// Pipe to another command.
		`|\|\s*\w` +
		// Hex/unicode escape sequences that could encode the above.
		`|\\x[0-9a-fA-F]{2}` +
		`|\\u[0-9a-fA-F]{4}`,
)

// IsSafeBashPattern reports whether pattern is acceptable as a Bash allow-list
// entry.  It applies allow-list semantics: the first token of the pattern must
// be a known-safe command name (after stripping any absolute path prefix), and
// the full pattern must not contain shell injection constructs.
//
// Callers should prefer IsSafeBashPattern over DangerousBashRE for new code.
// Returning false means the pattern is rejected; the reason is returned for
// diagnostic purposes.
func IsSafeBashPattern(pattern string) (bool, string) {
	if strings.TrimSpace(pattern) == "" {
		return false, "empty pattern"
	}

	// Extract the first token (the command name), honouring glob wildcards
	// that may appear as the entire pattern (e.g. "pnpm *").
	fields := strings.Fields(pattern)
	if len(fields) == 0 {
		return false, "empty pattern"
	}

	// Strip absolute path prefix so "/usr/bin/xargs" → "xargs".
	first := filepath.Base(fields[0])
	// Normalise to lower-case for case-insensitive comparison.
	firstLower := strings.ToLower(first)

	if ok, explicit := safeBashCommands[firstLower]; explicit && !ok {
		// Explicitly blocked (e.g. curl is in the map with value false).
		return false, "command explicitly blocked: " + first
	} else if !explicit {
		// Not in the allow-list at all.
		return false, "command not in safe allow-list: " + first
	}

	// Secondary check: reject shell injection constructs anywhere in the pattern.
	if shellInjectionRE.MatchString(pattern) {
		return false, "shell injection construct detected in pattern"
	}

	// Also run DangerousBashRE as a belt-and-suspenders check.
	if DangerousBashRE.MatchString(pattern) {
		return false, "dangerous bash pattern detected"
	}

	return true, ""
}

// ErrWebFetchPatternRequired is returned by ValidateWebFetchPattern when a
// WebFetch grant has no domain pattern, which would allow unrestricted HTTP
// requests and enable prompt-injection exfiltration.
var ErrWebFetchPatternRequired = errors.New(
	"WebFetch permission requires a non-empty domain pattern (e.g. \"https://docs.example.com*\"); " +
		"blanket WebFetch allows are rejected to prevent prompt-injection exfiltration",
)

// ValidateWebFetchPattern verifies that a WebFetch permission entry carries an
// explicit, non-empty URL/domain pattern.  Call this before granting or
// building an allow-list entry for any WebFetch permission.
//
// Returns ErrWebFetchPatternRequired when pattern is nil or empty.
func ValidateWebFetchPattern(pattern *string) error {
	if pattern == nil || strings.TrimSpace(*pattern) == "" {
		return ErrWebFetchPatternRequired
	}
	return nil
}

// WriteToolNames is the canonical list of write-type tools that trigger the edit gate.
// isWriteTool in the hooks handler derives from this slice — updating this list is sufficient.
var WriteToolNames = []string{"Edit", "Write", "MultiEdit"}

// IsAllowedTool reports whether name is in the pipeline tool allow-list.
// Use this instead of accessing allowedToolNames directly so the set
// cannot be mutated by other packages.
func IsAllowedTool(name string) bool { return allowedToolNames[name] }

// writeToolNames is the unexported set of tools that mutate file contents.
// External packages must use IsWriteTool to prevent mutation of the map.
var writeToolNames = map[string]bool{
	"Write":     true,
	"Edit":      true,
	"MultiEdit": true,
}

// IsWriteTool reports whether name is in the edit-gate write tool set.
// Use this instead of accessing writeToolNames directly so the set cannot
// be mutated by other packages.
func IsWriteTool(name string) bool { return writeToolNames[name] }

// allowedToolNames is the unexported source of truth for grantable tools.
// All callers must go through IsAllowedTool.
var allowedToolNames = map[string]bool{
	"Read":         true,
	"Write":        true,
	"Edit":         true,
	"MultiEdit":    true,
	"Glob":         true,
	"Grep":         true,
	"LS":           true,
	"Bash":         true,
	"WebFetch":     true,
	"WebSearch":    true,
	"Task":         true,
	"Agent":        true,
	"TodoRead":     true,
	"TodoWrite":    true,
	"NotebookRead": true,
	"NotebookEdit": true,
	"mcp__dashboard-channel__dashboard_reply":    true,
	"mcp__dashboard-channel__request_permission": true,
}
