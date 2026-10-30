package main

import (
	"crypto/subtle"
	"embed"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const adminDiscordID = "331819178275241984"

//go:embed admin.html analytics.html
var adminAssets embed.FS
var adminTemplate = template.Must(template.ParseFS(adminAssets, "admin.html"))

type adminHandler struct {
	auth      *uploadAuth
	directory string
	metadata  *SoundMetadataStore
	commitMu  *sync.Mutex
	publish   func(oldName, newName string)
}

type adminPage struct {
	SignedIn, Admin                bool
	Username, CSRF, Message, Error string
	Sounds                         []string
}

func (h *adminHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	if r.URL.Path != "/admin" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(405)
		return
	}
	session, err := h.auth.session(r)
	if err != nil {
		http.Error(w, "Could not load Discord login", 503)
		return
	}
	page := adminPage{}
	if session != nil {
		page.SignedIn = true
		page.Username = session.Username
		page.Admin = h.auth.configured() && session.ID == adminDiscordID
		page.CSRF = session.CSRF
	}
	if r.Method == http.MethodPost {
		if !page.Admin {
			http.Error(w, "Administrator access required", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid request", 400)
			return
		}
		csrf := r.PostForm.Get("csrf")
		if len(csrf) == 0 || len(csrf) != len(session.CSRF) || subtle.ConstantTimeCompare([]byte(csrf), []byte(session.CSRF)) != 1 {
			http.Error(w, "Invalid request. Reload the page and try again.", 403)
			return
		}
		action, name, newName := r.PostForm.Get("action"), r.PostForm.Get("name"), strings.TrimSpace(r.PostForm.Get("newName"))
		if action != "rename" && action != "delete" {
			http.Error(w, "Unknown action", 400)
			return
		}
		if action == "delete" {
			newName = ""
		}
		status, message := h.change(name, newName, action)
		if status != 0 {
			page.Error = message
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(status)
		} else {
			result := "renamed"
			if action == "delete" {
				result = "deleted"
			}
			http.Redirect(w, r, "/admin?done="+result, http.StatusSeeOther)
			return
		}
	}
	if page.Admin {
		entries, err := os.ReadDir(h.directory)
		if err != nil {
			http.Error(w, "Could not read sound library", 500)
			return
		}
		seen := map[string]bool{}
		for _, entry := range entries {
			if name, ok := librarySoundName(h.directory, entry.Name()); ok && !seen[name] {
				page.Sounds = append(page.Sounds, name)
				seen[name] = true
			}
		}
		sort.Slice(page.Sounds, func(i, j int) bool { return strings.ToLower(page.Sounds[i]) < strings.ToLower(page.Sounds[j]) })
		switch r.URL.Query().Get("done") {
		case "renamed":
			page.Message = "Sound renamed."
		case "deleted":
			page.Message = "Sound removed from the library."
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := adminTemplate.Execute(w, page); err != nil {
		log.Printf("[admin] rendering page: %v", err)
	}
}

func (h *adminHandler) change(name, newName, action string) (int, string) {
	// Existing library names can predate today's upload length limits.
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return 400, "Choose a valid library sound."
	}
	if action == "rename" && !validUploadName(newName) {
		return 400, "Use a sound name of 2–32 characters without slashes or control characters."
	}
	if name == newName {
		return 400, "Choose a different name."
	}
	h.commitMu.Lock()
	defer h.commitMu.Unlock()
	var files []string
	for _, ext := range []string{".mp3", ".ogg"} {
		filename := name + ext
		if _, ok := librarySoundName(h.directory, filename); ok {
			files = append(files, filename)
		} else if _, err := os.Lstat(filepath.Join(h.directory, filename)); err == nil {
			return 400, "This sound is not a regular audio file."
		} else if !os.IsNotExist(err) {
			return 500, "Could not check sound storage."
		}
		if newName != "" {
			if _, err := os.Lstat(filepath.Join(h.directory, newName+ext)); err == nil {
				return 409, "A sound with that name already exists."
			} else if !os.IsNotExist(err) {
				return 500, "Could not check sound storage."
			}
		}
	}
	if len(files) == 0 {
		return 404, "This sound is no longer in the library. Reload the page."
	}
	// Stage removals on the same filesystem; failed persistence restores the files.
	archive, err := os.MkdirTemp(h.directory, ".admin-archive-")
	if err != nil {
		return 500, "Could not prepare sound storage."
	}
	committed := false
	var moved []string
	var linked []string
	defer func() {
		restored := true
		if !committed {
			for _, file := range linked {
				os.Remove(filepath.Join(h.directory, newName+filepath.Ext(file)))
			}
			for _, file := range moved {
				if err := os.Rename(filepath.Join(archive, file), filepath.Join(h.directory, file)); err != nil {
					restored = false
					log.Printf("[admin] restoring %q: %v", file, err)
				}
			}
		}
		if restored && (!committed || newName != "") {
			os.RemoveAll(archive)
		}
	}()
	for _, file := range files {
		if err := os.Rename(filepath.Join(h.directory, file), filepath.Join(archive, file)); err != nil {
			return 500, "Could not move sound."
		}
		moved = append(moved, file)
		if newName != "" {
			if err := os.Link(filepath.Join(archive, file), filepath.Join(h.directory, newName+filepath.Ext(file))); err != nil {
				return 500, "Could not rename sound."
			}
			linked = append(linked, file)
		}
	}
	if err := h.metadata.changeLibraryName(name, newName, files); err != nil {
		log.Printf("[admin] saving library change: %v", err)
		return 500, "Could not save sound changes."
	}
	committed = true
	h.publish(name, newName)
	return 0, ""
}

func (m *SoundMetadataStore) changeLibraryName(oldName, newName string, files []string) error {
	m.flushMu.Lock()
	defer m.flushMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	entry, exists := m.data.Sounds[oldName]
	if newName != "" && exists {
		if _, err = tx.Exec("INSERT INTO sound_metadata(name,description,emoji_id,emoji_name) VALUES(?,?,?,?) ON CONFLICT(name) DO UPDATE SET description=excluded.description,emoji_id=excluded.emoji_id,emoji_name=excluded.emoji_name", newName, entry.Description, entry.EmojiID, entry.EmojiName); err != nil {
			return err
		}
		if _, err = tx.Exec("DELETE FROM sound_emoji_overrides WHERE name=?", newName); err != nil {
			return err
		}
		if entry.emojiEdited {
			if _, err = tx.Exec("INSERT INTO sound_emoji_overrides(name) VALUES(?)", newName); err != nil {
				return err
			}
		}
	}
	for _, table := range []string{"sound_metadata", "sound_emoji_overrides"} {
		if _, err = tx.Exec("DELETE FROM "+table+" WHERE name=?", oldName); err != nil {
			return err
		}
	}
	for _, file := range files {
		if newName == "" {
			_, err = tx.Exec("DELETE FROM sound_uploads WHERE filename=?", file)
		} else {
			_, err = tx.Exec("UPDATE sound_uploads SET filename=? WHERE filename=?", newName+filepath.Ext(file), file)
		}
		if err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	delete(m.data.Sounds, oldName)
	delete(m.dirty, oldName)
	if newName != "" && exists {
		m.data.Sounds[newName] = entry
		delete(m.dirty, newName)
	}
	m.version++
	m.encoded = nil
	return nil
}
