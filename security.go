package main

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var inlineScripts = regexp.MustCompile(`(?s)<script>(.*?)</script>`)

// Hash only trusted, shipped scripts, never request data or rendered templates.
// Build and restart together so the bootstrap hash follows the deployed HTML.
func securityHeaders(next http.Handler, directory string) http.Handler {
	scripts := []string{"'self'"}
	index, _ := os.ReadFile(filepath.Join(directory, "index.html"))
	admin, _ := adminAssets.ReadFile("admin.html")
	analytics, _ := adminAssets.ReadFile("analytics.html")
	for _, page := range [][]byte{index, admin, analytics} {
		for _, match := range inlineScripts.FindAllSubmatch(page, -1) {
			sum := sha256.Sum256(match[1])
			scripts = append(scripts, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
		}
	}
	policy := "default-src 'self'; script-src " + strings.Join(scripts, " ") + "; script-src-attr 'none'; style-src 'self' 'unsafe-inline'; img-src 'self' https://cdn.discordapp.com data:; media-src 'self' https://cdn.discordapp.com blob:; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", policy)
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
