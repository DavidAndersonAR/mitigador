package api

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:web_dist
var spaFS embed.FS

// staticHandler returns an http.Handler that serves web_dist/* with SPA
// fallback to index.html for paths that don't resolve to a real file.
//
// Security note (T-01-10-09): this catch-all is registered LAST in chi, after
// all /api/* routes. Future authenticated endpoints not under /api/ must be
// registered before this handler.
func staticHandler() http.Handler {
	sub, err := fs.Sub(spaFS, "web_dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "SPA not built", http.StatusInternalServerError)
		})
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /api/* should never reach here — chi routes /api/* before the catch-all.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(sub, name); err != nil {
			// SPA fallback: any unknown path returns index.html so vue-router handles it.
			indexBytes, err := fs.ReadFile(sub, "index.html")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(indexBytes)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
