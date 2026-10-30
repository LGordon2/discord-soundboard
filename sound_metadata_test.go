package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetadataPersistsDescriptionAndBothEmojiKinds(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "sounds.sqlite3")
	store, err := newSoundMetadataStore(testDatabase(t, filename))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	unicode, custom := "🦆", "1234567890"
	store.SetDescription("duck", "From a cartoon")
	if !store.SetEmoji("duck", nil, &unicode) {
		t.Fatal("emoji not captured")
	}
	if store.SetEmoji("duck", nil, &unicode) {
		t.Fatal("identical emoji marked dirty")
	}
	store.SetEmoji("custom", &custom, nil)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newSoundMetadataStore(testDatabase(t, filename))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Snapshot()["duck"] != (SoundMetadata{Description: "From a cartoon", EmojiName: unicode}) {
		t.Fatalf("metadata lost: %+v", reopened.Snapshot())
	}
	if reopened.Snapshot()["custom"].EmojiID != custom {
		t.Fatal("custom emoji lost")
	}
	copy := reopened.Snapshot()
	delete(copy, "duck")
	if reopened.Snapshot()["duck"].Description == "" {
		t.Fatal("snapshot was not isolated")
	}
	reopened.SetEmoji("duck", nil, nil)
	if reopened.Snapshot()["duck"].Description != "From a cartoon" || reopened.Snapshot()["duck"].EmojiName != "" {
		t.Fatal("clearing emoji lost description")
	}
}

func TestMetadataAppliedToUploadWithoutChangingCachedRequest(t *testing.T) {
	store, err := newSoundMetadataStore(testDatabase(t, filepath.Join(t.TempDir(), "sounds.sqlite3")))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, kind := range []string{"unicode", "custom"} {
		t.Run(kind, func(t *testing.T) {
			emoji := "🦆"
			if kind == "custom" {
				emoji = "12345"
				store.SetEmoji("name", &emoji, nil)
			} else {
				store.SetEmoji("name", nil, &emoji)
			}
			client := testDiscordClient(t, func(r *http.Request) (*http.Response, error) {
				var request CreateSoundboardSoundRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				if kind == "custom" {
					if request.EmojiID == nil || *request.EmojiID != emoji || request.EmojiName != nil {
						t.Fatalf("request=%+v", request)
					}
				} else {
					if request.EmojiName == nil || *request.EmojiName != emoji || request.EmojiID != nil {
						t.Fatalf("request=%+v", request)
					}
				}
				return &http.Response{StatusCode: 201, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"sound_id":"new"}`))}, nil
			})
			client.metadata = store
			cached := prepareSoundboardSound("name", "audio/ogg", []byte("audio"))
			if _, err := client.createSoundboardSound(guildID, cached); err != nil {
				t.Fatal(err)
			}
			if cached.EmojiID != nil || cached.EmojiName != nil {
				t.Fatal("cache mutated")
			}
		})
	}
}

func TestMetadataEditValidation(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "sounds.sqlite3")
	store, err := newSoundMetadataStore(testDatabase(t, filename))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	notified := 0
	handler := soundMetadataHandler(store, func(name string) bool { return name == "known" }, func() { notified++ })
	for _, tc := range []struct {
		method, body string
		status       int
	}{
		{"PUT", `{"soundName":"known","description":" A source & <script>text</script> "}`, 200},
		{"PUT", `{"soundName":"unknown","description":"x"}`, 400},
		{"PUT", `{"soundName":"known","description":"` + strings.Repeat("a", 2001) + `"}`, 400},
		{"PUT", `invalid`, 400},
		{"DELETE", "", 405},
		{"GET", "", 200},
	} {
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, httptest.NewRequest(tc.method, "/sound-metadata", strings.NewReader(tc.body)))
		if result.Code != tc.status {
			t.Errorf("%s: status=%d body=%s", tc.method, result.Code, result.Body.String())
		}
	}
	if notified != 1 {
		t.Fatalf("notifications=%d", notified)
	}
	var description string
	if err := store.db.QueryRow("SELECT description FROM sound_metadata WHERE name='known'").Scan(&description); err != nil {
		t.Fatal(err)
	}
	if description != "A source & <script>text</script>" {
		t.Fatal("description not persisted")
	}

}

func TestInvalidEmojiFallsBackWithoutDuplicatingUploadCount(t *testing.T) {
	store, err := newSoundMetadataStore(testDatabase(t, filepath.Join(t.TempDir(), "sounds.sqlite3")))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	emoji := "12345"
	store.SetEmoji("name", &emoji, nil)
	calls, count := 0, 0
	client := testDiscordClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		var payload CreateSoundboardSoundRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		status, body := 201, `{"sound_id":"new"}`
		if calls == 1 {
			status, body = 400, `{"code":50035,"errors":{"emoji_id":{"_errors":[{"code":"INVALID_EMOJI"}]}}}`
		} else if payload.EmojiID != nil || payload.EmojiName != nil {
			t.Fatal("retry still has rejected emoji")
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	client.metadata = store
	client.onSoundCreated = func(string) { count++ }
	if _, err := client.CreateSoundboardSound(guildID, "name", "audio/ogg", []byte("audio")); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || count != 1 {
		t.Fatalf("calls=%d count=%d", calls, count)
	}
	if store.Snapshot()["name"].EmojiID != "" {
		t.Fatal("rejected emoji was retained")
	}
}

func TestMetadataETagRevalidationAndEdits(t *testing.T) {
	db := testDatabase(t, filepath.Join(t.TempDir(), "metadata.sqlite3"))
	store, err := newSoundMetadataStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler := soundMetadataHandler(store, func(name string) bool { return name == "known" }, nil)
	request := func(method, tag, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/sound-metadata", strings.NewReader(body))
		if tag != "" {
			req.Header.Set("If-None-Match", tag)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	initial := request("GET", "", "")
	tag := initial.Header().Get("ETag")
	if initial.Code != 200 || tag == "" || initial.Header().Get("Cache-Control") != "private, no-cache, no-transform" {
		t.Fatal("missing cache policy or ETag")
	}
	for _, validator := range []string{tag, "W/" + tag, `"other", ` + tag, "*"} {
		cached := request("GET", validator, "")
		if cached.Code != 304 || cached.Body.Len() != 0 || cached.Header().Get("ETag") != tag {
			t.Fatalf("validator %s: status=%d body=%s", validator, cached.Code, cached.Body.String())
		}
	}
	head := request("HEAD", "", "")
	if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("ETag") != tag {
		t.Fatal("HEAD incorrect")
	}
	edited := request("PUT", tag, `{"soundName":"known","description":"New description"}`)
	newTag := edited.Header().Get("ETag")
	if edited.Code != 200 || newTag == tag || edited.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("PUT must update despite read validator")
	}
	changed := request("GET", tag, "")
	if changed.Code != 200 || changed.Header().Get("ETag") != newTag || !strings.Contains(changed.Body.String(), "New description") {
		t.Fatal("description edit not invalidated")
	}
	store.SetDescription("known", "New description")
	if request("GET", newTag, "").Code != 304 {
		t.Fatal("no-op edit changed validator")
	}
	emoji := "😂"
	store.SetEmoji("known", nil, &emoji)
	withEmoji := request("GET", newTag, "")
	if withEmoji.Code != 200 || withEmoji.Header().Get("ETag") == newTag {
		t.Fatal("emoji edit not invalidated")
	}
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := newSoundMetadataStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	_, reloadedTag := reloaded.representation()
	if reloadedTag != withEmoji.Header().Get("ETag") {
		t.Fatal("validator changed across reload")
	}
}

func TestEmojiEditingPersistsAndSurvivesGatewaySnapshots(t *testing.T) {
	db := testDatabase(t, filepath.Join(t.TempDir(), "emoji-edit.sqlite3"))
	store, err := newSoundMetadataStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	oldEmoji := "😂"
	store.SetEmoji("known", nil, &oldEmoji)
	handler := soundMetadataHandler(store, func(name string) bool { return name == "known" }, nil)
	update := func(body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("PUT", "/sound-metadata", strings.NewReader(body)))
		return response
	}
	_, oldTag := store.representation()
	if response := update(`{"soundName":"known","description":"Source","emoji":"👩🏽‍🚀"}`); response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	entry := store.Snapshot()["known"]
	if entry.EmojiName != "👩🏽‍🚀" || entry.EmojiID != "" || !entry.emojiEdited {
		t.Fatalf("wrong edit: %+v", entry)
	}
	if store.SetEmoji("known", nil, &oldEmoji) {
		t.Fatal("Gateway overwrote user choice")
	}
	_, tag := store.representation()
	if tag == oldTag {
		t.Fatal("ETag not invalidated")
	}
	reloaded, err := newSoundMetadataStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	if !reloaded.Snapshot()["known"].emojiEdited || reloaded.SetEmoji("known", nil, &oldEmoji) {
		t.Fatal("override not persisted")
	}
	request := reloaded.ApplyEmoji(CreateSoundboardSoundRequest{Name: "known"})
	if request.EmojiName == nil || *request.EmojiName != "👩🏽‍🚀" {
		t.Fatal("future upload lost edited emoji")
	}
	if response := update(`{"soundName":"known","description":"Source","emoji":"<a:party:12345>"}`); response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	entry = store.Snapshot()["known"]
	if entry.EmojiID != "12345" || entry.EmojiName != "" {
		t.Fatal("custom emoji not normalized")
	}
	if response := update(`{"soundName":"known","description":"Old client edit"}`); response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if store.Snapshot()["known"].EmojiID != "12345" {
		t.Fatal("description-only edit erased emoji")
	}
	for _, emoji := range []string{"😂🎉", "👩🏽‍🚀👍🏽", "🇺🇸🇨🇦", "1️⃣2️⃣", "<:a:12345><:b:67890>", "hello", "<script>alert(1)</script>", "18446744073709551616", strings.Repeat("😂", 33)} {
		body, _ := json.Marshal(map[string]string{"soundName": "known", "description": "should not apply", "emoji": emoji})
		if update(string(body)).Code != 400 {
			t.Fatalf("invalid emoji accepted: %q", emoji)
		}
		if store.Snapshot()["known"].Description != "Old client edit" {
			t.Fatal("invalid emoji partially changed metadata")
		}
	}
	if update(`{"soundName":"known","description":"Source","emoji":""}`).Code != 200 {
		t.Fatal("clear failed")
	}
	entry = store.Snapshot()["known"]
	if entry.EmojiID != "" || entry.EmojiName != "" || store.SetEmoji("known", nil, &oldEmoji) {
		t.Fatal("explicit removal overwritten")
	}
	// A rejected user-selected custom emoji may still be cleared by upload fallback.
	store.editDetails("known", "Source", true, "12345", "")
	if !store.setEmoji("known", nil, nil, true) {
		t.Fatal("rejected emoji fallback could not clear override")
	}
	if store.SetEmoji("known", nil, &oldEmoji) {
		t.Fatal("fallback removal lost override")
	}
}

func TestEditedEmojiInputs(t *testing.T) {
	for _, value := range []string{"☕️", "1️⃣", "🇺🇸", "👍🏽", "👨‍👩‍👧‍👦", "‼️", "⁉️"} {
		id, emoji, err := parseEditedEmoji(value)
		if err != nil || id != "" || emoji != value {
			t.Fatalf("valid Unicode rejected: %s %v", value, err)
		}
	}
}

func TestEmojiOverrideSchemaUpgradePreservesMetadata(t *testing.T) {
	db := testDatabase(t, filepath.Join(t.TempDir(), "old.sqlite3"))
	if _, err := db.Exec("DROP TABLE sound_emoji_overrides"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO sound_metadata(name,description,emoji_name) VALUES('old','Existing description','😂')"); err != nil {
		t.Fatal(err)
	}
	if err := initializeDatabase(db, "", ""); err != nil {
		t.Fatal(err)
	}
	store, err := newSoundMetadataStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entry := store.Snapshot()["old"]
	if entry.Description != "Existing description" || entry.EmojiName != "😂" || entry.emojiEdited {
		t.Fatal("existing metadata changed during upgrade")
	}
	store.editDetails("old", entry.Description, true, "", "🦆")
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sound_emoji_overrides WHERE name='old'").Scan(&count); err != nil || count != 1 {
		t.Fatal("new override did not persist")
	}
}
