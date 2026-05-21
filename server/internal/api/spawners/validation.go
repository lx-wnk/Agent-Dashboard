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

// ValidAdapterTypes is the application-layer allow-list of adapter_type values.
// The ent schema does not enforce this (string column with default "claude");
// validation lives here and at the repo entry points.
var ValidAdapterTypes = []string{"claude", "ollama", "openai", "custom"}

// allowedAdapterConfigKeys lists permitted adapter_config keys per adapter_type.
// Required keys are listed in requiredAdapterConfigKeys below.
//
// - claude:  no adapter_config keys (config flows via command/args/env columns).
// - ollama:  optional host, default_model.
// - openai:  required api_key_env; optional base_url, default_model.
// - custom:  no adapter_config keys; reuses the row's command/args/env.
var allowedAdapterConfigKeys = map[string]map[string]struct{}{
	"claude": {},
	"ollama": {
		"host":          {},
		"default_model": {},
	},
	"openai": {
		"api_key_env":   {},
		"base_url":      {},
		"default_model": {},
	},
	"custom": {},
}

var requiredAdapterConfigKeys = map[string][]string{
	"openai": {"api_key_env"},
}

// ValidateAdapterType returns "", true when t is one of ValidAdapterTypes,
// otherwise an error message suitable for a 400 response.
func ValidateAdapterType(t string) (string, bool) {
	for _, v := range ValidAdapterTypes {
		if t == v {
			return "", true
		}
	}
	return "adapter_type must be one of claude|ollama|openai|custom", false
}

// ValidateAdapterConfig enforces:
//   - adapterType must be a known type
//   - every key must be in the allow-list for the given adapter_type
//   - every required key must be present and non-empty
//   - keys must be non-empty, free of shell metacharacters
//   - values must be <= 4096 chars
func ValidateAdapterConfig(adapterType string, cfg map[string]string) (string, bool) {
	allowed, ok := allowedAdapterConfigKeys[adapterType]
	if !ok {
		return "adapter_type must be one of claude|ollama|openai|custom", false
	}
	for k, v := range cfg {
		if k == "" {
			return "adapter_config key must be non-empty", false
		}
		if envKeyForbidden.MatchString(k) {
			return "adapter_config key contains forbidden characters", false
		}
		if _, ok := allowed[k]; !ok {
			return "unknown adapter_config key: " + k, false
		}
		if len(v) > envValueMaxLen {
			return "adapter_config value exceeds 4096 chars", false
		}
	}
	for _, req := range requiredAdapterConfigKeys[adapterType] {
		if v, ok := cfg[req]; !ok || v == "" {
			return "adapter_config missing required key: " + req, false
		}
	}
	return "", true
}

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
