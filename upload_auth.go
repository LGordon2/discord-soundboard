package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type uploadIdentity struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}
type uploadSession struct {
	uploadIdentity
	CSRF    string
	Expires int64
}
type uploadAuth struct {
	db                                  *sql.DB
	clientID, clientSecret, redirectURI string
	allowlistFile, allowedIDs           string
	client                              *http.Client
	tokenURL, userURL, authorizeURL     string
	now                                 func() time.Time
}

func randomUploadToken() (string, error) {
	var token [32]byte
	_, err := rand.Read(token[:])
	return base64.RawURLEncoding.EncodeToString(token[:]), err
}
func uploadTokenHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func (a *uploadAuth) configured() bool {
	return a.clientID != "" && a.clientSecret != "" && a.redirectURI != ""
}
func (a *uploadAuth) secure() bool { return strings.HasPrefix(a.redirectURI, "https://") }
func (a *uploadAuth) cookie(w http.ResponseWriter, name, value string, seconds int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Secure: a.secure(), HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: seconds})
}
func (a *uploadAuth) allowed(id string) bool {
	value := a.allowedIDs
	if value == "" {
		data, err := os.ReadFile(a.allowlistFile)
		if err != nil {
			return false
		}
		value = string(data)
	}
	for _, candidate := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t' }) {
		if candidate == id {
			return true
		}
	}
	return false
}
func (a *uploadAuth) session(r *http.Request) (*uploadSession, error) {
	cookie, err := r.Cookie("soundboard_upload_session")
	if err != nil {
		return nil, nil
	}
	if len(cookie.Value) != 43 {
		return nil, nil
	}
	var session uploadSession
	err = a.db.QueryRow("SELECT user_id,username,csrf,expires FROM upload_sessions WHERE token_hash=? AND expires>?", uploadTokenHash(cookie.Value), a.now().Unix()).Scan(&session.ID, &session.Username, &session.CSRF, &session.Expires)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}
func (a *uploadAuth) authorize(r *http.Request) (uploadIdentity, int) {
	if !a.configured() {
		return uploadIdentity{}, 503
	}
	session, err := a.session(r)
	if err != nil {
		return uploadIdentity{}, 503
	}
	if session == nil {
		return uploadIdentity{}, 401
	}
	if !a.allowed(session.ID) {
		return uploadIdentity{}, 403
	}
	supplied := r.Header.Get("X-CSRF-Token")
	if len(supplied) != len(session.CSRF) || subtle.ConstantTimeCompare([]byte(supplied), []byte(session.CSRF)) != 1 {
		return uploadIdentity{}, 403
	}
	return session.uploadIdentity, 0
}
func (a *uploadAuth) register(mux *http.ServeMux) {
	mux.HandleFunc("/auth/session", a.status)
	mux.HandleFunc("/auth/discord", a.login)
	mux.HandleFunc("/auth/discord/callback", a.callback)
	mux.HandleFunc("/auth/logout", a.logout)
}
func (a *uploadAuth) status(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(405)
		return
	}
	session, err := a.session(r)
	if err != nil {
		http.Error(w, "Could not load Discord login", 503)
		return
	}
	result := map[string]any{"configured": a.configured(), "canUpload": false}
	if session != nil {
		result["user"] = session.uploadIdentity
		result["csrf"] = session.CSRF
		result["canUpload"] = a.configured() && a.allowed(session.ID)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
func (a *uploadAuth) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(405)
		return
	}
	if !a.configured() {
		http.Error(w, "Discord upload login is not configured", 503)
		return
	}
	state, err := randomUploadToken()
	if err != nil {
		http.Error(w, "Could not begin Discord login", 500)
		return
	}
	a.cookie(w, "soundboard_upload_oauth_state", state, 600)
	query := url.Values{"response_type": {"code"}, "client_id": {a.clientID}, "scope": {"identify"}, "redirect_uri": {a.redirectURI}, "state": {state}}
	http.Redirect(w, r, a.authorizeURL+"?"+query.Encode(), 302)
}
func (a *uploadAuth) callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	state, err := r.Cookie("soundboard_upload_oauth_state")
	a.cookie(w, "soundboard_upload_oauth_state", "", -1)
	provided := r.URL.Query().Get("state")
	if err != nil || len(state.Value) != 43 || len(provided) != 43 || subtle.ConstantTimeCompare([]byte(state.Value), []byte(provided)) != 1 {
		http.Error(w, "Discord login expired. Start again.", 400)
		return
	}
	if r.URL.Query().Get("error") != "" {
		http.Redirect(w, r, "/?upload_login=cancelled", 303)
		return
	}
	code := r.URL.Query().Get("code")
	if !a.configured() || code == "" {
		http.Error(w, "Could not complete Discord login", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	identity, err := a.exchange(ctx, code)
	if err != nil {
		log.Printf("[upload-auth] Discord identity verification failed: %v", err)
		http.Error(w, "Could not verify Discord login. Please try again.", 502)
		return
	}
	token, err := randomUploadToken()
	if err != nil {
		http.Error(w, "Could not create login session", 500)
		return
	}
	csrf, err := randomUploadToken()
	if err != nil {
		http.Error(w, "Could not create login session", 500)
		return
	}
	// Rotate the current browser session and remove expired records.
	tx, err := a.db.Begin()
	if err != nil {
		http.Error(w, "Could not save login session", 500)
		return
	}
	defer tx.Rollback()
	if old, cookieErr := r.Cookie("soundboard_upload_session"); cookieErr == nil {
		if _, err = tx.Exec("DELETE FROM upload_sessions WHERE token_hash=?", uploadTokenHash(old.Value)); err != nil {
			http.Error(w, "Could not save login session", 500)
			return
		}
	}
	if _, err = tx.Exec("DELETE FROM upload_sessions WHERE expires<=?", a.now().Unix()); err != nil {
		http.Error(w, "Could not save login session", 500)
		return
	}
	if _, err = tx.Exec("INSERT INTO upload_sessions(token_hash,user_id,username,csrf,expires) VALUES(?,?,?,?,?)", uploadTokenHash(token), identity.ID, identity.Username, csrf, a.now().Add(7*24*time.Hour).Unix()); err != nil {
		http.Error(w, "Could not save login session", 500)
		return
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "Could not save login session", 500)
		return
	}
	a.cookie(w, "soundboard_upload_session", token, 7*24*3600)
	http.Redirect(w, r, "/?upload_login=success", 303)
}
func (a *uploadAuth) exchange(ctx context.Context, code string) (uploadIdentity, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {a.redirectURI}}
	request, err := http.NewRequestWithContext(ctx, "POST", a.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return uploadIdentity{}, err
	}
	request.SetBasicAuth(a.clientID, a.clientSecret)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := a.client.Do(request)
	if err != nil {
		return uploadIdentity{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return uploadIdentity{}, fmt.Errorf("token exchange failed with HTTP %d", response.StatusCode)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&token); err != nil || token.AccessToken == "" {
		return uploadIdentity{}, fmt.Errorf("invalid token response")
	}
	userRequest, err := http.NewRequestWithContext(ctx, "GET", a.userURL, nil)
	if err != nil {
		return uploadIdentity{}, err
	}
	userRequest.Header.Set("Authorization", "Bearer "+token.AccessToken)
	userResponse, err := a.client.Do(userRequest)
	if err != nil {
		return uploadIdentity{}, err
	}
	defer userResponse.Body.Close()
	if userResponse.StatusCode != 200 {
		return uploadIdentity{}, fmt.Errorf("identity request failed with HTTP %d", userResponse.StatusCode)
	}
	var identity uploadIdentity
	if err = json.NewDecoder(io.LimitReader(userResponse.Body, 65536)).Decode(&identity); err != nil || !customEmojiID.MatchString(identity.ID) || identity.Username == "" {
		return uploadIdentity{}, fmt.Errorf("invalid Discord identity")
	}
	return identity, nil
}
func (a *uploadAuth) logout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(405)
		return
	}
	session, err := a.session(r)
	if err != nil {
		http.Error(w, "Could not sign out", 503)
		return
	}
	if session != nil {
		supplied := r.Header.Get("X-CSRF-Token")
		if len(supplied) != len(session.CSRF) || subtle.ConstantTimeCompare([]byte(supplied), []byte(session.CSRF)) != 1 {
			http.Error(w, "Invalid sign out request", 403)
			return
		}
		cookie, _ := r.Cookie("soundboard_upload_session")
		if _, err = a.db.Exec("DELETE FROM upload_sessions WHERE token_hash=?", uploadTokenHash(cookie.Value)); err != nil {
			http.Error(w, "Could not sign out", 503)
			return
		}
	}
	a.cookie(w, "soundboard_upload_session", "", -1)
	w.WriteHeader(204)
}
