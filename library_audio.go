package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func librarySoundName(directory, filename string) (string, bool) {
	if filename == "" || strings.ContainsAny(filename, `/\`) || filepath.Base(filename) != filename {
		return "", false
	}
	ext := filepath.Ext(filename)
	if ext != ".ogg" && ext != ".mp3" {
		return "", false
	}
	info, err := os.Lstat(filepath.Join(directory, filename))
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return "", false
	}
	return strings.TrimSuffix(filename, ext), true
}

func libraryAudioHandler(directory string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(405)
			return
		}
		filename := r.URL.Query().Get("file")
		if _, ok := librarySoundName(directory, filename); !ok {
			http.NotFound(w, r)
			return
		}
		file, err := os.Open(filepath.Join(directory, filename))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		mime := "audio/ogg"
		if filepath.Ext(filename) == ".mp3" {
			mime = "audio/mpeg"
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Set("ETag", fmt.Sprintf(`W/"%x-%x"`, info.ModTime().UnixNano(), info.Size()))
		http.ServeContent(w, r, filename, info.ModTime(), file)
	})
}
