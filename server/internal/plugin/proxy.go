package plugin

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// NewReverseProxy returns an http.Handler that proxies requests to the plugin's addr.
// The incoming path prefix /api/plugins/{id} is stripped before proxying so that
// route_extension plugins receive paths relative to their own root.
// Sensitive headers (Cookie, Authorization) are stripped before forwarding to prevent
// a compromised plugin from exfiltrating user sessions.
func NewReverseProxy(entry Entry) http.Handler {
	target, err := url.Parse("http://" + entry.Descriptor.Addr)
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "plugin address invalid", http.StatusServiceUnavailable)
		})
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Rewrite = func(r *httputil.ProxyRequest) {
		r.SetURL(target)
		r.Out.Header.Del("Cookie")
		r.Out.Header.Del("Authorization")
	}
	prefix := "/api/plugins/" + entry.Descriptor.ID
	return http.StripPrefix(prefix, proxy)
}
