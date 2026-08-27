package capability

import (
	"fmt"
	"net/url"
	"strings"
)

// Pattern is a validated grant pattern.
type Pattern string

// ParsePattern validates and returns a pattern, or an error if the pattern is
// malformed. An empty string is valid: it is the wildcard. Patterns prefixed
// with "domain:" must have a non-empty, valid hostname remainder.
func ParsePattern(s string) (Pattern, error) {
	// Empty string is valid: the wildcard
	if s == "" {
		return Pattern(""), nil
	}

	// domain: prefix requires a valid hostname
	if strings.HasPrefix(s, "domain:") {
		domain := strings.TrimPrefix(s, "domain:")
		if domain == "" {
			return "", fmt.Errorf("domain pattern must have a non-empty hostname after 'domain:'")
		}
		if !isValidHostname(domain) {
			return "", fmt.Errorf("invalid hostname in domain pattern: %q", domain)
		}
		return Pattern(s), nil
	}

	// Exact and prefix patterns are always valid
	return Pattern(s), nil
}

// isValidHostname checks whether a string is a plausible hostname.
// A valid hostname contains only alphanumeric characters, dots, and hyphens,
// does not start or end with a dot or hyphen, and has at least one character.
func isValidHostname(host string) bool {
	if host == "" {
		return false
	}
	if host[0] == '.' || host[0] == '-' {
		return false
	}
	if host[len(host)-1] == '.' || host[len(host)-1] == '-' {
		return false
	}
	for _, r := range host {
		if !isHostnameChar(r) {
			return false
		}
	}
	return true
}

// isHostnameChar reports whether r is a valid character in a hostname:
// alphanumeric, dot, or hyphen.
func isHostnameChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == '.' || r == '-'
}

// Match reports whether a grant's pattern covers a requested value.
// An empty grant pattern is a wildcard, matching the nil-pattern convention the
// permission tables already use.
func Match(grantPattern, requested string) bool {
	switch {
	case grantPattern == "":
		return true
	case strings.HasPrefix(grantPattern, "domain:"):
		return matchDomain(strings.TrimPrefix(grantPattern, "domain:"), requested)
	case strings.HasSuffix(grantPattern, "*"):
		return strings.HasPrefix(requested, strings.TrimSuffix(grantPattern, "*"))
	default:
		return grantPattern == requested
	}
}

// matchDomain matches a host or any of its subdomains. It compares label by
// label rather than by string suffix, so "example.com" does not match
// "evilexample.com". Domain comparison is case-insensitive.
func matchDomain(domain, requested string) bool {
	u, err := url.Parse(requested)
	if err != nil || u.Host == "" {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	// DNS is case-insensitive; compare lowercase
	domain = strings.ToLower(domain)
	host = strings.ToLower(host)
	return host == domain || strings.HasSuffix(host, "."+domain)
}
