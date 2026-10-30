package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLibraryAudioStreamingAndValidation(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string]string{"Quote & 😂.ogg": "OggSaudio-data", "same.mp3": "mp3-audio", "empty.ogg": "", "secret.txt": "secret"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "secret.txt"), filepath.Join(dir, "link.ogg")); err != nil {
		t.Fatal(err)
	}
	handler := libraryAudioHandler(dir)
	request := func(method, file, rangeHeader, tag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/library-audio", nil)
		q := req.URL.Query()
		q.Set("file", file)
		req.URL.RawQuery = q.Encode()
		if rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}
		if tag != "" {
			req.Header.Set("If-None-Match", tag)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	first := request("GET", "Quote & 😂.ogg", "", "")
	if first.Code != 200 || first.Body.String() != "OggSaudio-data" || first.Header().Get("Content-Type") != "audio/ogg" {
		t.Fatalf("bad Ogg response: %+v", first)
	}
	partial := request("GET", "same.mp3", "bytes=0-2", "")
	if partial.Code != 206 || partial.Body.String() != "mp3" || partial.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatal("MP3 range failed")
	}
	if result := request("HEAD", "same.mp3", "", ""); result.Code != 200 || result.Body.Len() != 0 {
		t.Fatal("HEAD failed")
	}
	if result := request("GET", "Quote & 😂.ogg", "", first.Header().Get("ETag")); result.Code != 304 || result.Body.Len() != 0 {
		t.Fatal("revalidation failed")
	}
	for _, name := range []string{"", "../same.mp3", "sub/file.ogg", `..\same.mp3`, "empty.ogg", "secret.txt", "missing.ogg", "link.ogg"} {
		if result := request("GET", name, "", ""); result.Code != 404 {
			t.Fatalf("unsafe/unknown file %q: %d", name, result.Code)
		}
	}
	if request("POST", "same.mp3", "", "").Code != 405 {
		t.Fatal("method restriction failed")
	}
}
func TestLibraryPlaybackAnalytics(t *testing.T) {
	analytics, err := newLocalAnalytics(testDatabase(t, filepath.Join(t.TempDir(), "analytics.sqlite3")))
	if err != nil {
		t.Fatal(err)
	}
	defer analytics.Close()
	handler := analyticsHandler(analytics, func(string) (string, bool) { return "", false }, func(file string) (string, bool) { return "Local sound", file == "Local sound.ogg" })
	for _, test := range []struct {
		body   string
		status int
	}{
		{`{"soundLocation":"Local sound.ogg"}`, 204},
		{`{"soundLocation":"../private.ogg"}`, 400},
		{`{"soundLocation":"Local sound.ogg","soundID":"ambiguous"}`, 400},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/analytics/play", strings.NewReader(test.body)))
		if response.Code != test.status {
			t.Fatalf("status=%d", response.Code)
		}
	}
	if analytics.Snapshot().Totals.Played != 1 || analytics.Snapshot().Sounds["Local sound"].Played != 1 {
		t.Fatal("wrong local play count")
	}
}
