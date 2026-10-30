package main

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

var hashedAssetPath = regexp.MustCompile(`^/assets/[^/]+-[A-Za-z0-9_-]{8}\.[a-zA-Z0-9]+$`)

// Hashed URLs change with their content. Keep the HTML revalidated so a refresh
// picks up the newest bundle, while unchanged JS/CSS/images need no round trip.
func staticHandler(directory string) http.Handler {
	files := http.FileServer(http.Dir(directory))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		if hashedAssetPath.MatchString(r.URL.Path) {
			if info, err := os.Stat(filepath.Join(directory, r.URL.Path[1:])); err == nil && !info.IsDir() {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
		}
		files.ServeHTTP(w, r)
	})
}
