package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

func standaloneDashboard(assets fs.FS) http.Handler {
	files := spaFileServer(assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dashboard/wa" || strings.HasPrefix(r.URL.Path, "/dashboard/wa/") {
			http.Redirect(w, r, "/", http.StatusMovedPermanently)
			return
		}
		files.ServeHTTP(w, r)
	})
}

func spaFileServer(assets fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if info, err := fs.Stat(assets, name); err == nil && !info.IsDir() {
			http.ServeFileFS(w, r, assets, name)
			return
		}
		if strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)
			return
		}
		http.ServeFileFS(w, r, assets, "index.html")
	})
}
