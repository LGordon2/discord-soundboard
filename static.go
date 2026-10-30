package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var hashedAssetPath = regexp.MustCompile(`^/assets/[^/]+-[A-Za-z0-9_-]{8}\.[a-zA-Z0-9]+$`)

// Hashed URLs change with their content. Keep the HTML revalidated so a refresh
// picks up the newest bundle, while unchanged JS/CSS/images need no round trip.
func staticHandler(directory string, initialDescriptions ...func() [soundboardSoundCount]bool) http.Handler {
	files := http.FileServer(http.Dir(directory))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		if r.URL.Path == "/" && len(initialDescriptions) > 0 && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			if page, err := os.ReadFile(filepath.Join(directory, "index.html")); err == nil {
				var layout strings.Builder
				for slot, described := range initialDescriptions[0]() {
					if described {
						fmt.Fprintf(&layout, "html.ui-preview #soundboard-%d{height:9rem;min-height:9rem}", slot)
					}
				}
				if layout.Len() > 0 {
					style := `<style id="initial-card-layout">` + layout.String() + `</style></head>`
					page = bytes.Replace(page, []byte("</head>"), []byte(style), 1)
				}
				// The layout hint varies with current descriptions, so its validator
				// must cover the generated page rather than the index file mtime.
				sum := sha256.Sum256(page)
				w.Header().Set("ETag", fmt.Sprintf(`"%x"`, sum[:16]))
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(page))
				return
			}
		}
		if hashedAssetPath.MatchString(r.URL.Path) {
			if info, err := os.Stat(filepath.Join(directory, r.URL.Path[1:])); err == nil && !info.IsDir() {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
		}
		files.ServeHTTP(w, r)
	})
}
