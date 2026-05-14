package plugin

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// NewReverseProxy returns an http.Handler that proxies requests to the plugin's addr.
// The incoming path prefix /api/plugins/{id} is stripped before proxying so that
// route_extension plugins receive paths relative to their own root.
func NewReverseProxy(entry Entry) http.Handler {
	target, _ := url.Parse("http://" + entry.Descriptor.Addr)
	proxy := httputil.NewSingleHostReverseProxy(target)
	prefix := "/api/plugins/" + entry.Descriptor.ID
	return http.StripPrefix(prefix, proxy)
}
