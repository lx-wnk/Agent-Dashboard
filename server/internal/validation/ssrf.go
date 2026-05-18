package validation

import (
	"context"
	"fmt"
	"net"
	"time"
)

// ssrfBlockedHosts is the set of hostnames that are always blocked regardless
// of DNS resolution outcome.
var ssrfBlockedHosts = map[string]struct{}{
	"localhost": {},
	"127.0.0.1": {},
	"::1":       {},
	"0.0.0.0":   {},
}

// cgnatBlock covers the Carrier-Grade NAT range (100.64.0.0/10), which is
// commonly used by VPNs (e.g. Tailscale) and must not be reachable via SSRF.
var cgnatBlock = func() *net.IPNet {
	_, n, _ := net.ParseCIDR("100.64.0.0/10")
	return n
}()

// IsBlockedIP reports whether ip must not be dialled from the server
// (loopback, link-local, private, unspecified, multicast, or CGNAT).
func IsBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() ||
		ip.IsMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		cgnatBlock.Contains(ip)
}

// IsBlockedHost reports whether the plain hostname string is in the static
// block-list (before DNS resolution).
func IsBlockedHost(host string) bool {
	_, blocked := ssrfBlockedHosts[host]
	return blocked
}

// SafeDialContext is a net.Dialer.DialContext replacement that re-validates
// resolved IPs at connection time to prevent DNS rebinding attacks.
//
// It resolves DNS, checks every returned address against IsBlockedIP, and only
// connects when all resolved IPs are safe. This guards against the case where a
// domain passes a static URL check but later resolves to a private/loopback
// address.
func SafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("ssrf: no IPs resolved for %s", host)
	}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if IsBlockedIP(ip) {
			return nil, fmt.Errorf("ssrf: resolved IP %s is blocked", ipStr)
		}
	}
	return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0], port))
}
