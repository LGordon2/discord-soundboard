package main

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	root := t.TempDir()
	script := `document.documentElement.classList.add("ui-preview")`
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<script>"+script+"</script>"), 0600); err != nil {
		t.Fatal(err)
	}
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}), root)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/missing", nil))
	for header, want := range map[string]string{
		"Strict-Transport-Security":  "max-age=31536000; includeSubDomains",
		"Cross-Origin-Opener-Policy": "same-origin",
		"X-Frame-Options":            "DENY",
		"X-Content-Type-Options":     "nosniff",
	} {
		if got := response.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	policy := response.Header().Get("Content-Security-Policy")
	sum := sha256.Sum256([]byte(script))
	if !strings.Contains(policy, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'") {
		t.Fatal("CSP does not authorize the shipped bootstrap")
	}
	for _, directive := range []string{"script-src-attr 'none'", "object-src 'none'", "base-uri 'none'", "frame-ancestors 'none'", "media-src 'self' https://cdn.discordapp.com blob:"} {
		if !strings.Contains(policy, directive) {
			t.Errorf("missing directive %s", directive)
		}
	}
	if response.Code != http.StatusNotFound {
		t.Fatal("middleware changed handler status")
	}
}
