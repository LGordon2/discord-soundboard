package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticCachePolicy(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"index.html": "<html>current</html>", "assets/index-Ab12_cd9.js": "console.log('ok')"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(staticHandler(root))
	defer server.Close()
	for _, tc := range []struct {
		path, policy string
		status       int
	}{
		{"/", "no-cache, no-transform", 200},
		{"/assets/index-Ab12_cd9.js", "public, max-age=31536000, immutable", 200},
		{"/assets/missing-Ab12_cd9.js", "no-cache, no-transform", 404},
		{"/missing", "no-cache, no-transform", 404},
	} {
		response, err := http.Get(server.URL + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != tc.status || response.Header.Get("Cache-Control") != tc.policy {
			t.Errorf("%s: status=%d policy=%q", tc.path, response.StatusCode, response.Header.Get("Cache-Control"))
		}
	}
	request, _ := http.NewRequest("GET", server.URL+"/assets/index-Ab12_cd9.js", nil)
	request.Header.Set("Range", "bytes=0-6")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 206 || string(body) != "console" {
		t.Fatalf("range serving broke: %d %q", response.StatusCode, body)
	}
}

func TestInitialDescriptionLayoutAndConditionalRevalidation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte(`<!doctype html><html><head></head><body>Board</body></html>`), 0600); err != nil {
		t.Fatal(err)
	}
	var described [soundboardSoundCount]bool
	described[2] = true
	handler := staticHandler(root, func() [soundboardSoundCount]bool { return described })
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(first.Body.String(), "#soundboard-2{height:7.75rem") || strings.Contains(first.Body.String(), "#soundboard-0{") {
		t.Fatalf("wrong layout hint: %s", first.Body.String())
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("generated page lacks a validator")
	}
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("If-None-Match", etag)
	same := httptest.NewRecorder()
	handler.ServeHTTP(same, request)
	if same.Code != http.StatusNotModified {
		t.Fatalf("unchanged layout: %d", same.Code)
	}
	described[2] = false
	changed := httptest.NewRecorder()
	handler.ServeHTTP(changed, request)
	if changed.Code != http.StatusOK || changed.Header().Get("ETag") == etag || strings.Contains(changed.Body.String(), "initial-card-layout") {
		t.Fatalf("stale layout cached: %d %s", changed.Code, changed.Body.String())
	}
}
