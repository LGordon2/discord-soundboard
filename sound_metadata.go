package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

type SoundMetadata struct {
	Description string `json:"description"`
	emojiEdited bool
	EmojiID     string `json:"emoji_id,omitempty"`
	EmojiName   string `json:"emoji_name,omitempty"`
}

type soundMetadataFile struct {
	Version int                      `json:"version"`
	Sounds  map[string]SoundMetadata `json:"sounds"`
}

type SoundMetadataStore struct {
	mu                    sync.RWMutex
	flushMu               sync.Mutex
	data                  soundMetadataFile
	db                    *sql.DB
	dirty                 map[string]uint64
	version, savedVersion uint64
	stop, done            chan struct{}
	closeOnce             sync.Once
	encodedVersion        uint64
	encoded               []byte
	etag                  string
}

func newSoundMetadataStore(db *sql.DB) (*SoundMetadataStore, error) {
	m := &SoundMetadataStore{db: db, data: soundMetadataFile{Version: 1, Sounds: make(map[string]SoundMetadata)}, dirty: make(map[string]uint64), stop: make(chan struct{}), done: make(chan struct{})}
	rows, err := db.Query("SELECT m.name,m.description,m.emoji_id,m.emoji_name,o.name IS NOT NULL FROM sound_metadata m LEFT JOIN sound_emoji_overrides o ON o.name=m.name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var entry SoundMetadata
		if err := rows.Scan(&name, &entry.Description, &entry.EmojiID, &entry.EmojiName, &entry.emojiEdited); err != nil {
			return nil, err
		}
		m.data.Sounds[name] = entry
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	go func() {
		defer close(m.done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := m.Flush(); err != nil {
					log.Printf("[warn] saving sound metadata: %v", err)
				}
			case <-m.stop:
				return
			}
		}
	}()
	return m, nil
}

func (m *SoundMetadataStore) snapshotLocked() map[string]SoundMetadata {
	result := make(map[string]SoundMetadata, len(m.data.Sounds))
	for name, value := range m.data.Sounds {
		result[name] = value
	}
	return result
}
func (m *SoundMetadataStore) Snapshot() map[string]SoundMetadata {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshotLocked()
}

// Cache the representation until metadata changes. The content hash remains
// stable across process restarts and is tied to the exact bytes served.
func (m *SoundMetadataStore) representation() ([]byte, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.encoded == nil || m.encodedVersion != m.version {
		data, err := json.Marshal(m.data.Sounds)
		if err != nil {
			panic(err)
		} // SoundMetadata contains only strings.
		sum := sha256.Sum256(data)
		m.encoded = data
		m.etag = fmt.Sprintf(`"%x"`, sum[:16])
		m.encodedVersion = m.version
	}
	return m.encoded, m.etag
}

func (m *SoundMetadataStore) SetDescription(name, description string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.data.Sounds[name]
	if entry.Description == description {
		return
	}
	entry.Description = description
	m.data.Sounds[name] = entry
	m.version++
	m.dirty[name] = m.version
}
func (m *SoundMetadataStore) SetEmoji(name string, id, emoji *string) bool {
	return m.setEmoji(name, id, emoji, false)
}
func (m *SoundMetadataStore) setEmoji(name string, id, emoji *string, force bool) bool {
	if name == "" {
		return false
	}
	value := SoundMetadata{}
	if id != nil {
		value.EmojiID = *id
	}
	if value.EmojiID == "" && emoji != nil {
		value.EmojiName = *emoji
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old := m.data.Sounds[name]
	if old.emojiEdited && !force {
		return false
	}
	if old.EmojiID == value.EmojiID && old.EmojiName == value.EmojiName {
		return false
	}
	old.EmojiID, old.EmojiName = value.EmojiID, value.EmojiName
	m.data.Sounds[name] = old
	m.version++
	m.dirty[name] = m.version
	return true
}
func (m *SoundMetadataStore) ApplyEmoji(request CreateSoundboardSoundRequest) CreateSoundboardSoundRequest {
	m.mu.RLock()
	entry, ok := m.data.Sounds[request.Name]
	m.mu.RUnlock()
	if !ok {
		return request
	}
	request.EmojiID, request.EmojiName = nil, nil
	if entry.EmojiID != "" {
		request.EmojiID = &entry.EmojiID
	} else if entry.EmojiName != "" {
		request.EmojiName = &entry.EmojiName
	}
	return request
}
func (m *SoundMetadataStore) Flush() error {
	m.flushMu.Lock()
	defer m.flushMu.Unlock()
	m.mu.RLock()
	if m.version == m.savedVersion {
		m.mu.RUnlock()
		return nil
	}
	version := m.version
	changed := make(map[string]SoundMetadata, len(m.dirty))
	for name := range m.dirty {
		changed[name] = m.data.Sounds[name]
	}
	m.mu.RUnlock()
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statement, err := tx.Prepare("INSERT INTO sound_metadata(name,description,emoji_id,emoji_name) VALUES(?,?,?,?) ON CONFLICT(name) DO UPDATE SET description=excluded.description,emoji_id=excluded.emoji_id,emoji_name=excluded.emoji_name")
	if err != nil {
		return err
	}
	defer statement.Close()
	for name, entry := range changed {
		if entry.emojiEdited {
			if _, err = tx.Exec("INSERT OR IGNORE INTO sound_emoji_overrides(name) VALUES(?)", name); err != nil {
				return err
			}
		}
		if _, err = statement.Exec(name, entry.Description, entry.EmojiID, entry.EmojiName); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	m.mu.Lock()
	m.savedVersion = version
	for name, revision := range m.dirty {
		if revision <= version {
			delete(m.dirty, name)
		}
	}
	m.mu.Unlock()
	return nil
}
func (m *SoundMetadataStore) Close() error {
	m.closeOnce.Do(func() { close(m.stop) })
	<-m.done
	return m.Flush()
}

var customEmojiMarkup = regexp.MustCompile(`^<a?:[A-Za-z0-9_]{1,32}:([0-9]+)>$`)
var customEmojiID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

func parseEditedEmoji(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", nil
	}
	if match := customEmojiMarkup.FindStringSubmatch(value); match != nil {
		value = match[1]
	}
	if customEmojiID.MatchString(value) {
		if _, err := strconv.ParseUint(value, 10, 64); err == nil {
			return value, "", nil
		}
	}
	if utf8.RuneCountInString(value) <= 32 {
		hasSymbol, valid := false, true
		for _, r := range value {
			switch {
			case unicode.IsSymbol(r), r == '\u203c', r == '\u2049':
				hasSymbol = true
			case unicode.IsMark(r), r == '\u200d', r == '#', r == '*', r >= '0' && r <= '9':
				if r == '\u20e3' {
					hasSymbol = true
				}
			default:
				valid = false
			}
		}
		if valid && hasSymbol {
			return "", value, nil
		}
	}
	return "", "", fmt.Errorf("Use a Unicode emoji, Discord custom emoji ID, or <:name:id>; leave blank to remove")
}
func (m *SoundMetadataStore) editDetails(name, description string, editEmoji bool, id, emoji string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.data.Sounds[name]
	next := entry
	next.Description = description
	if editEmoji {
		next.EmojiID, next.EmojiName, next.emojiEdited = id, emoji, true
	}
	if next == entry {
		return
	}
	m.data.Sounds[name] = next
	m.version++
	m.dirty[name] = m.version
}

func soundMetadataHandler(store *SoundMetadataStore, knownName func(string) bool, changed func()) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPut {
			w.Header().Set("Allow", "GET, HEAD, PUT")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if store == nil {
			http.Error(w, "Sound details unavailable", 503)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodPut {
			input := struct {
				SoundName   string  `json:"soundName"`
				Description string  `json:"description"`
				Emoji       *string `json:"emoji"`
			}{}
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16000))
			if err := decoder.Decode(&input); err != nil || utf8.RuneCountInString(input.Description) > 2000 || !knownName(input.SoundName) {
				http.Error(w, "Invalid sound or description (maximum 2000 characters)", 400)
				return
			}
			var id, emoji string
			if input.Emoji != nil {
				var err error
				id, emoji, err = parseEditedEmoji(*input.Emoji)
				if err != nil {
					http.Error(w, err.Error(), 400)
					return
				}
			}
			store.editDetails(input.SoundName, strings.TrimSpace(input.Description), input.Emoji != nil, id, emoji)
			if err := store.Flush(); err != nil {
				log.Printf("[warn] saving description: %v", err)
				http.Error(w, "Could not save sound details", 500)
				return
			}
			if changed != nil {
				changed()
			}
		}
		data, etag := store.representation()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", etag)
		if r.Method == http.MethodPut {
			w.Write(data)
			return
		}
		w.Header().Set("Cache-Control", "private, no-cache, no-transform")
		http.ServeContent(w, r, "sound-metadata.json", time.Time{}, bytes.NewReader(data))
	})
}

// Persist newly uploaded details before publishing them to in-memory readers.
func (m *SoundMetadataStore) saveUploadedDetails(name, description, id, emoji string, user uploadIdentity) error {
	m.flushMu.Lock()
	defer m.flushMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO sound_metadata(name,description,emoji_id,emoji_name) VALUES(?,?,?,?) ON CONFLICT(name) DO UPDATE SET description=excluded.description,emoji_id=excluded.emoji_id,emoji_name=excluded.emoji_name", name, description, id, emoji); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT OR IGNORE INTO sound_emoji_overrides(name) VALUES(?)", name); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO sound_uploads(filename,uploader_id,uploader_name,uploaded_at) VALUES(?,?,?,?) ON CONFLICT(filename) DO UPDATE SET uploader_id=excluded.uploader_id,uploader_name=excluded.uploader_name,uploaded_at=excluded.uploaded_at", name+".mp3", user.ID, user.Username, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	m.data.Sounds[name] = SoundMetadata{Description: description, EmojiID: id, EmojiName: emoji, emojiEdited: true}
	m.version++
	m.dirty[name] = m.version
	return nil
}
