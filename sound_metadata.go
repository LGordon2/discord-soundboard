package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type SoundMetadata struct {
	Description string `json:"description"`
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
}

func newSoundMetadataStore(db *sql.DB) (*SoundMetadataStore, error) {
	m := &SoundMetadataStore{db: db, data: soundMetadataFile{Version: 1, Sounds: make(map[string]SoundMetadata)}, dirty: make(map[string]uint64), stop: make(chan struct{}), done: make(chan struct{})}
	rows, err := db.Query("SELECT name,description,emoji_id,emoji_name FROM sound_metadata")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var entry SoundMetadata
		if err := rows.Scan(&name, &entry.Description, &entry.EmojiID, &entry.EmojiName); err != nil {
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

func soundMetadataHandler(store *SoundMetadataStore, knownName func(string) bool, changed func()) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPut {
			w.Header().Set("Allow", "GET, PUT")
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
				SoundName   string `json:"soundName"`
				Description string `json:"description"`
			}{}
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16000))
			if err := decoder.Decode(&input); err != nil || utf8.RuneCountInString(input.Description) > 2000 || !knownName(input.SoundName) {
				http.Error(w, "Invalid sound or description (maximum 2000 characters)", 400)
				return
			}
			store.SetDescription(input.SoundName, strings.TrimSpace(input.Description))
			if err := store.Flush(); err != nil {
				log.Printf("[warn] saving description: %v", err)
				http.Error(w, "Could not save sound details", 500)
				return
			}
			if changed != nil {
				changed()
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(store.Snapshot())
	})
}
