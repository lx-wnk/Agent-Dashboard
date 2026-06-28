package plugin

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ProxyResolver is the registry behaviour the dispatcher needs. Declared here so
// tests can fake it without standing up real processes.
type ProxyResolver interface {
	Lookup(id string) (Entry, bool)
}

// NewDispatcher returns the single catch-all handler mounted at
// /api/plugins/{id}/proxy/*. It resolves the live registry per request — chi
// freezes its route tree after ListenAndServe (chi #480), so live enable/disable
// cannot add or remove routes; routing must be data, not router structure.
//
// Responses: 400 for a malformed id; 503 when the plugin is not currently
// serving (stopped, unhealthy, or not a route/ui extension); otherwise the
// request is reverse-proxied with Cookie/Authorization stripped (NewReverseProxy).
func NewDispatcher(res ProxyResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !pluginIDRe.MatchString(id) {
			http.Error(w, "invalid plugin id", http.StatusBadRequest)
			return
		}
		entry, ok := res.Lookup(id)
		if !ok || !entry.healthy {
			http.Error(w, "plugin not available", http.StatusServiceUnavailable)
			return
		}
		if !entry.Descriptor.HasCapability(CapRouteExtension) && !entry.Descriptor.HasCapability(CapUIExtension) {
			http.Error(w, "plugin not available", http.StatusServiceUnavailable)
			return
		}
		NewReverseProxy(entry, "/api/plugins/"+id+"/proxy").ServeHTTP(w, r)
	})
}
