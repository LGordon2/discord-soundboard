package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testUploadAuth(t *testing.T) (*uploadAuth, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			calls.Add(1)
			id, secret, _ := r.BasicAuth()
			if id != "app-id" || secret != "app-secret" {
				t.Error("wrong OAuth app credentials")
			}
			r.ParseForm()
			if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "test-code" || r.Form.Get("redirect_uri") != "https://soundboard.example/auth/discord/callback" {
				t.Error("invalid OAuth exchange")
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"access_token":"oauth-access-token"}`))
		case "/me":
			if r.Header.Get("Authorization") != "Bearer oauth-access-token" {
				t.Error("identity not authenticated")
			}
			w.Write([]byte(`{"id":"12345","username":"approved-user","avatar":"abc123"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(provider.Close)
	allowlist := filepath.Join(t.TempDir(), "allowlist")
	if err := os.WriteFile(allowlist, []byte("12345\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &uploadAuth{db: testDatabase(t, filepath.Join(t.TempDir(), "auth.sqlite3")), clientID: "app-id", clientSecret: "app-secret", redirectURI: "https://soundboard.example/auth/discord/callback", allowlistFile: allowlist, client: provider.Client(), tokenURL: provider.URL + "/token", userURL: provider.URL + "/me", authorizeURL: "https://discord.com/oauth2/authorize", now: time.Now}
	return a, calls
}
func loginTestUploader(t *testing.T, a *uploadAuth) *http.Cookie {
	t.Helper()
	begin := httptest.NewRecorder()
	a.login(begin, httptest.NewRequest("GET", "/auth/discord", nil))
	if begin.Code != 302 {
		t.Fatal("login did not redirect")
	}
	uri, err := url.Parse(begin.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if uri.Query().Get("scope") != "identify" || uri.Query().Get("client_id") != "app-id" || strings.Contains(begin.Header().Get("Location"), "app-secret") {
		t.Fatal("invalid login scope or leaked secret")
	}
	state := begin.Result().Cookies()[0]
	if !state.HttpOnly || !state.Secure || state.SameSite != http.SameSiteLaxMode {
		t.Fatal("state cookie policy")
	}
	req := httptest.NewRequest("GET", "/auth/discord/callback?"+url.Values{"state": {state.Value}, "code": {"test-code"}}.Encode(), nil)
	req.AddCookie(state)
	result := httptest.NewRecorder()
	a.callback(result, req)
	if result.Code != 303 {
		t.Fatalf("callback: %d %s", result.Code, result.Body.String())
	}
	for _, cookie := range result.Result().Cookies() {
		if cookie.Name == "soundboard_upload_session" {
			if !cookie.HttpOnly || !cookie.Secure {
				t.Fatal("session cookie policy")
			}
			return cookie
		}
	}
	t.Fatal("no session cookie")
	return nil
}
func sessionTestRequest(cookie *http.Cookie, csrf string) *http.Request {
	req := httptest.NewRequest("POST", "/upload-sound", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	req.Header.Set("X-CSRF-Token", csrf)
	return req
}
func TestDiscordUploadOAuthSessionAndAllowlist(t *testing.T) {
	a, _ := testUploadAuth(t)
	cookie := loginTestUploader(t, a)
	statusRequest := httptest.NewRequest("GET", "/auth/session", nil)
	statusRequest.AddCookie(cookie)
	status := httptest.NewRecorder()
	a.status(status, statusRequest)
	var value struct {
		CanUpload bool           `json:"canUpload"`
		User      uploadIdentity `json:"user"`
		CSRF      string         `json:"csrf"`
	}
	if json.Unmarshal(status.Body.Bytes(), &value) != nil || !value.CanUpload || value.User.ID != "12345" || value.User.Avatar != "abc123" || value.CSRF == "" {
		t.Fatalf("bad status: %s", status.Body.String())
	}
	if strings.Contains(status.Body.String(), "oauth-access-token") || status.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("OAuth token leaked or status cached")
	}
	identity, code := a.authorize(sessionTestRequest(cookie, value.CSRF))
	if code != 0 || identity.ID != "12345" {
		t.Fatal("approved identity rejected")
	}
	if _, code = a.authorize(sessionTestRequest(cookie, "wrong")); code != 403 {
		t.Fatal("missing CSRF protection")
	}
	if _, code = a.authorize(sessionTestRequest(nil, value.CSRF)); code != 401 {
		t.Fatal("anonymous upload allowed")
	}
	restarted := *a
	if _, code = restarted.authorize(sessionTestRequest(cookie, value.CSRF)); code != 0 {
		t.Fatal("session did not survive restart")
	}
	var rawCount int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM upload_sessions WHERE token_hash=?", cookie.Value).Scan(&rawCount); err != nil || rawCount != 0 {
		t.Fatal("raw session cookie persisted")
	}
	if err := os.WriteFile(a.allowlistFile, []byte("99999\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, code = a.authorize(sessionTestRequest(cookie, value.CSRF)); code != 403 {
		t.Fatal("revoked user still allowed")
	}
	if err := os.WriteFile(a.allowlistFile, []byte("12345\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a.now = func() time.Time { return time.Now().Add(8 * 24 * time.Hour) }
	if _, code = a.authorize(sessionTestRequest(cookie, value.CSRF)); code != 401 {
		t.Fatal("expired session allowed")
	}
}
func TestDiscordUploadOAuthStateAndErrors(t *testing.T) {
	a, calls := testUploadAuth(t)
	for _, query := range []string{"code=test-code", "code=test-code&state=wrong", "error=access_denied"} {
		response := httptest.NewRecorder()
		a.callback(response, httptest.NewRequest("GET", "/auth/discord/callback?"+query, nil))
		if response.Code != 400 {
			t.Fatal("callback without state accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("bad state reached token exchange")
	}
	token, _ := randomUploadToken()
	request := httptest.NewRequest("GET", "/auth/discord/callback?"+url.Values{"state": {token}, "error": {"access_denied"}}.Encode(), nil)
	request.AddCookie(&http.Cookie{Name: "soundboard_upload_oauth_state", Value: token})
	response := httptest.NewRecorder()
	a.callback(response, request)
	if response.Code != 303 || calls.Load() != 0 {
		t.Fatal("consent cancellation not handled")
	}
	a.clientSecret = ""
	response = httptest.NewRecorder()
	a.login(response, httptest.NewRequest("GET", "/auth/discord", nil))
	if response.Code != 503 {
		t.Fatal("unconfigured OAuth allowed")
	}
	if _, code := a.authorize(sessionTestRequest(nil, "")); code != 503 {
		t.Fatal("unconfigured uploads allowed")
	}
}
func TestDiscordUploadLogout(t *testing.T) {
	a, _ := testUploadAuth(t)
	cookie := loginTestUploader(t, a)
	session, err := a.session(sessionTestRequest(cookie, ""))
	if err != nil || session == nil {
		t.Fatal("no session")
	}
	denied := httptest.NewRecorder()
	a.logout(denied, sessionTestRequest(cookie, "wrong"))
	if denied.Code != 403 {
		t.Fatal("logout CSRF accepted")
	}
	result := httptest.NewRecorder()
	a.logout(result, sessionTestRequest(cookie, session.CSRF))
	if result.Code != 204 {
		t.Fatal("logout failed")
	}
	if _, status := a.authorize(sessionTestRequest(cookie, session.CSRF)); status != 401 {
		t.Fatal("logged out session valid")
	}
}
