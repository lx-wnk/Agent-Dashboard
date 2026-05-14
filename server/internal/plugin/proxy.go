package plugin

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// NewReverseProxy returns an http.Handler that proxies requests to the plugin's addr.
// The incoming path prefix /api/plugins/{id} is stripped before proxying.
func NewReverseProxy(entry Entry) http.Handler {
	target, _ := url.Parse(entry.BaseURL)
	proxy := httputil.NewSingleHostReverseProxy(target)
	return proxy
}
