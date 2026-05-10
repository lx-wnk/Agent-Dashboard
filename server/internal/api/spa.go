package api

import (
	"io/fs"
	"net/http"
)

// NewSPAHandler returns an http.Handler that serves static files from fsys.
// For paths with no matching file, it serves index.html to support
// Vue Router history mode (all client-side routes fall through).
func NewSPAHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := fsys.Open(r.URL.Path)
		if err != nil {
			// Path not found — serve index.html for Vue Router
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/index.html"
			fileServer.ServeHTTP(w, r2)
			return
		}
		f.Close()
		fileServer.ServeHTTP(w, r)
	})
}
