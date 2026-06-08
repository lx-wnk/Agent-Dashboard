package plugin

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// NewReverseProxy returns an http.Handler that proxies requests to the plugin's addr.
// stripPrefix is stripped from the incoming request path before proxying so that
// route_extension plugins receive paths relative to their own root.
// Sensitive headers (Cookie, Authorization) are stripped before forwarding to prevent
// a compromised plugin from exfiltrating user sessions.
func NewReverseProxy(entry Entry, stripPrefix string) http.Handler {
	target, err := url.Parse("http://" + entry.Descriptor.Addr)
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "plugin address invalid", http.StatusServiceUnavailable)
		})
	}
	// Use Rewrite only — never combine with Director (NewSingleHostReverseProxy sets
	// Director, and net/http/httputil rejects a ReverseProxy that has both set,
	// failing every request with 502). r.SetURL replaces what the Director did.
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Header.Del("Cookie")
			r.Out.Header.Del("Authorization")
		},
		ModifyResponse: func(resp *http.Response) error {
			// Drop the upstream Content-Length. The gzip middleware wrapping this
			// handler recompresses the body, so the plugin's byte count no longer
			// matches what the client receives — leaving it set yields
			// ERR_CONTENT_LENGTH_MISMATCH in the browser. Without it the response is
			// sent chunked (and gzipped), so the length is never advertised stale.
			resp.Header.Del("Content-Length")
			return nil
		},
	}
	return http.StripPrefix(stripPrefix, proxy)
}
