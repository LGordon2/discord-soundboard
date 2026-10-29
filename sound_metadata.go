package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
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
	filename              string
	version, savedVersion uint64
	stop, done            chan struct{}
	closeOnce             sync.Once
}

func newSoundMetadataStore(filename string) (*SoundMetadataStore, error) {
	m := &SoundMetadataStore{filename: filename, data: soundMetadataFile{Version: 1, Sounds: make(map[string]SoundMetadata)}, version: 1, stop: make(chan struct{}), done: make(chan struct{})}
	data, err := os.ReadFile(filename)
	if err == nil {
		if err := json.Unmarshal(data, &m.data); err != nil {
			return nil, err
		}
		if m.data.Version != 1 {
			return nil, fmt.Errorf("invalid sound metadata format")
		}
		if m.data.Sounds == nil {
			m.data.Sounds = make(map[string]SoundMetadata)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return nil, err
	}
	if err := m.Flush(); err != nil {
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
	snapshot := soundMetadataFile{Version: 1, Sounds: m.snapshotLocked()}
	m.mu.RUnlock()
	if err := atomicWriteJSON(m.filename, snapshot); err != nil {
		return err
	}
	m.mu.Lock()
	m.savedVersion = version
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
