package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func testAdmin(t *testing.T) (*adminHandler, *http.Cookie, string, *int) {
	t.Helper()
	auth, _ := testUploadAuth(t)
	cookie := loginTestUploader(t, auth)
	if _, err := auth.db.Exec("UPDATE upload_sessions SET user_id=?", adminDiscordID); err != nil {
		t.Fatal(err)
	}
	metadata, err := newSoundMetadataStore(auth.db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { metadata.Close() })
	session, err := auth.session(sessionTestRequest(cookie, ""))
	if err != nil || session == nil {
		t.Fatal(err)
	}
	count := new(int)
	return &adminHandler{auth: auth, directory: t.TempDir(), metadata: metadata, commitMu: &sync.Mutex{}, publish: func(string, string) { *count++ }}, cookie, session.CSRF, count
}
func adminRequest(h *adminHandler, cookie *http.Cookie, csrf, action, name, newName string) *httptest.ResponseRecorder {
	form := url.Values{"csrf": {csrf}, "action": {action}, "name": {name}, "newName": {newName}}
	req := httptest.NewRequest("POST", "/admin", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	return out
}
func writeAdminSound(t *testing.T, h *adminHandler, file string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(h.directory, file), []byte("audio"), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestAdminAccessAndCSRF(t *testing.T) {
	h, cookie, csrf, count := testAdmin(t)
	writeAdminSound(t, h, "Private sound.mp3")
	for _, tc := range []struct {
		cookie *http.Cookie
		token  string
	}{{nil, csrf}, {cookie, ""}, {cookie, "incorrect"}} {
		if r := adminRequest(h, tc.cookie, tc.token, "delete", "Private sound", ""); r.Code != 403 {
			t.Fatalf("unauthorized mutation: %d", r.Code)
		}
	}
	req := httptest.NewRequest("GET", "/admin", nil)
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if strings.Contains(out.Body.String(), "Private sound") || !strings.Contains(out.Body.String(), "next=/admin") {
		t.Fatal("logged-out page leaks library or lacks login")
	}
	if _, err := h.auth.db.Exec("UPDATE upload_sessions SET user_id='99999',username='lewg9'"); err != nil {
		t.Fatal(err)
	}
	if r := adminRequest(h, cookie, csrf, "delete", "Private sound", ""); r.Code != 403 {
		t.Fatal("username granted admin access")
	}
	req = httptest.NewRequest("GET", "/admin", nil)
	req.AddCookie(cookie)
	out = httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if strings.Contains(out.Body.String(), "Private sound") {
		t.Fatal("non-admin sees library")
	}
	if *count != 0 {
		t.Fatal("unauthorized publish")
	}
	if _, err := h.auth.db.Exec("UPDATE upload_sessions SET user_id=?,expires=0", adminDiscordID); err != nil {
		t.Fatal(err)
	}
	if r := adminRequest(h, cookie, csrf, "delete", "Private sound", ""); r.Code != 403 {
		t.Fatal("expired session accepted")
	}
}
func TestAdminRenameVariantsMetadataAndDelete(t *testing.T) {
	h, cookie, csrf, count := testAdmin(t)
	for _, ext := range []string{".mp3", ".ogg"} {
		writeAdminSound(t, h, "Original"+ext)
	}
	if err := h.metadata.saveUploadedDetails("Original", "Description", "", "🦆", uploadIdentity{ID: "uploader", Username: "Uploader"}); err != nil {
		t.Fatal(err)
	}
	h.metadata.SetDescription("Original", "Latest unflushed description")
	out := adminRequest(h, cookie, csrf, "rename", "Original", "New <name>")
	if out.Code != 303 {
		t.Fatalf("rename failed: %d %s", out.Code, out.Body.String())
	}
	for _, ext := range []string{".mp3", ".ogg"} {
		if _, err := os.Stat(filepath.Join(h.directory, "New <name>"+ext)); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(h.directory, "Original"+ext)); !os.IsNotExist(err) {
			t.Fatal("old file remains")
		}
	}
	entry := h.metadata.Snapshot()["New <name>"]
	if entry.Description != "Latest unflushed description" || entry.EmojiName != "🦆" || !entry.emojiEdited {
		t.Fatalf("metadata lost: %+v", entry)
	}
	if err := h.metadata.Flush(); err != nil {
		t.Fatal(err)
	}
	var description, filename string
	if err := h.auth.db.QueryRow("SELECT description FROM sound_metadata WHERE name=?", "New <name>").Scan(&description); err != nil || description != entry.Description {
		t.Fatal("metadata not persisted", err)
	}
	if err := h.auth.db.QueryRow("SELECT filename FROM sound_uploads").Scan(&filename); err != nil || filename != "New <name>.mp3" {
		t.Fatal("uploader provenance lost", err)
	}
	req := httptest.NewRequest("GET", "/admin", nil)
	req.AddCookie(cookie)
	out = httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if strings.Contains(out.Body.String(), "New <name>") || !strings.Contains(out.Body.String(), "New &lt;name&gt;") {
		t.Fatal("admin sound names not escaped")
	}
	out = adminRequest(h, cookie, csrf, "delete", "New <name>", "")
	if out.Code != 303 || *count != 2 {
		t.Fatalf("delete failed: %d", out.Code)
	}
	for _, ext := range []string{".mp3", ".ogg"} {
		if _, err := os.Stat(filepath.Join(h.directory, "New <name>"+ext)); !os.IsNotExist(err) {
			t.Fatal("deleted file remains")
		}
	}
	archives, err := filepath.Glob(filepath.Join(h.directory, ".admin-archive-*", "*.mp3"))
	if err != nil || len(archives) != 1 {
		t.Fatal("deleted sound not archived", err)
	}
	var n int
	h.auth.db.QueryRow("SELECT count(*) FROM sound_metadata WHERE name IN (?,?)", "Original", "New <name>").Scan(&n)
	if n != 0 {
		t.Fatal("stale metadata remains")
	}
}
func TestAdminValidationAndRollback(t *testing.T) {
	h, cookie, csrf, count := testAdmin(t)
	writeAdminSound(t, h, "Original.mp3")
	writeAdminSound(t, h, "Existing.ogg")
	for _, tc := range []struct {
		action, name, newName string
		status                int
	}{{"rename", "Original", "Existing", 409}, {"rename", "Original", "../escape", 400}, {"delete", "../Original", "", 400}, {"rename", "Missing", "Valid", 404}, {"rename", "Original", "Original", 400}, {"rename", "Original", "x", 400}} {
		if out := adminRequest(h, cookie, csrf, tc.action, tc.name, tc.newName); out.Code != tc.status {
			t.Fatalf("%+v got %d", tc, out.Code)
		}
	}
	if err := h.metadata.saveUploadedDetails("Original", "Description", "", "", uploadIdentity{ID: "uploader", Username: "Uploader"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.auth.db.Exec("CREATE TRIGGER fail_admin BEFORE DELETE ON sound_metadata BEGIN SELECT RAISE(ABORT,'test failure'); END"); err != nil {
		t.Fatal(err)
	}
	if out := adminRequest(h, cookie, csrf, "rename", "Original", "Renamed"); out.Code != 500 {
		t.Fatal("expected persistence failure")
	}
	if data, err := os.ReadFile(filepath.Join(h.directory, "Original.mp3")); err != nil || string(data) != "audio" {
		t.Fatal("failed rename lost audio", err)
	}
	if _, err := os.Stat(filepath.Join(h.directory, "Renamed.mp3")); !os.IsNotExist(err) {
		t.Fatal("failed rename left target")
	}
	if *count != 0 {
		t.Fatal("failed mutation published")
	}
}
func TestAdminOAuthReturn(t *testing.T) {
	auth, _ := testUploadAuth(t)
	begin := httptest.NewRecorder()
	auth.login(begin, httptest.NewRequest("GET", "/auth/discord?next=/admin", nil))
	req := httptest.NewRequest("GET", "/auth/discord/callback?code=test-code&state="+begin.Result().Cookies()[0].Value, nil)
	for _, cookie := range begin.Result().Cookies() {
		req.AddCookie(cookie)
	}
	result := httptest.NewRecorder()
	auth.callback(result, req)
	if result.Code != 303 || result.Header().Get("Location") != "/admin" {
		t.Fatalf("admin login redirect: %d %s", result.Code, result.Header().Get("Location"))
	}
}
