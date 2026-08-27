package capability

import (
	"net/url"
	"strings"
)

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
// "evilexample.com".
func matchDomain(domain, requested string) bool {
	u, err := url.Parse(requested)
	if err != nil || u.Host == "" {
		return false
	}
	host := u.Hostname()
	return host == domain || strings.HasSuffix(host, "."+domain)
}
