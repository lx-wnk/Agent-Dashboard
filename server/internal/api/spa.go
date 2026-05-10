package api

import (
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
)

// NewSPAHandler returns an http.Handler that serves static files from fsys.
// For paths with no matching file, it serves index.html to support
// Vue Router history mode (all client-side routes fall through).
// Paths with a file extension that are not found return HTTP 404 instead of
// falling back to index.html.
func NewSPAHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// fs.FS requires unrooted paths; treat bare "/" as "index.html".
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		f, err := fsys.Open(path)
		if err != nil {
			// Path not found — only fall back to SPA for extension-less routes.
			if filepath.Ext(r.URL.Path) != "" {
				http.NotFound(w, r)
				return
			}
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/index.html"
			fileServer.ServeHTTP(w, r2)
			return
		}

		// Opened successfully — check whether it is a directory.
		stat, statErr := f.Stat()
		_ = f.Close()
		if statErr == nil && stat.IsDir() {
			// Directory: treat as not-found for SPA purposes.
			if filepath.Ext(r.URL.Path) != "" {
				http.NotFound(w, r)
				return
			}
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/index.html"
			fileServer.ServeHTTP(w, r2)
			return
		}

		fileServer.ServeHTTP(w, r)
	})
}
