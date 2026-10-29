package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type SoundCounts struct {
	Added  uint64 `json:"added"`
	Sent   uint64 `json:"sent"`
	Played uint64 `json:"played"`
}

type AnalyticsSnapshot struct {
	Version   int                    `json:"version"`
	Since     time.Time              `json:"since"`
	UpdatedAt time.Time              `json:"updated_at"`
	Totals    SoundCounts            `json:"totals"`
	Sounds    map[string]SoundCounts `json:"sounds"`
}

// Record only touches memory. A single writer persists atomic snapshots, keeping
// disk I/O out of the Discord request path and bounding storage to aggregates.
type LocalAnalytics struct {
	mu           sync.RWMutex
	flushMu      sync.Mutex
	state        AnalyticsSnapshot
	version      uint64
	savedVersion uint64
	filename     string
	stop         chan struct{}
	done         chan struct{}
	closeOnce    sync.Once
}

func newLocalAnalytics(filename string) (*LocalAnalytics, error) {
	now := time.Now().UTC()
	a := &LocalAnalytics{
		state:   AnalyticsSnapshot{Version: 1, Since: now, UpdatedAt: now, Sounds: make(map[string]SoundCounts)},
		version: 1, filename: filename, stop: make(chan struct{}), done: make(chan struct{}),
	}
	data, err := os.ReadFile(filename)
	switch {
	case err == nil:
		if err = json.Unmarshal(data, &a.state); err != nil {
			return nil, fmt.Errorf("reading analytics: %w", err)
		}
		if a.state.Version != 1 || a.state.Since.IsZero() {
			return nil, fmt.Errorf("invalid analytics file format")
		}
		if a.state.Sounds == nil {
			a.state.Sounds = make(map[string]SoundCounts)
		}
	case os.IsNotExist(err):
	default:
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return nil, err
	}
	// Verify storage is writable now rather than silently losing counts later.
	if err := a.Flush(); err != nil {
		return nil, err
	}
	go func() {
		defer close(a.done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := a.Flush(); err != nil {
					log.Printf("[warn] saving local analytics: %v", err)
				}
			case <-a.stop:
				return
			}
		}
	}()
	return a, nil
}

func incrementCounts(counts *SoundCounts, event string) bool {
	switch event {
	case "added":
		counts.Added++
	case "sent":
		counts.Sent++
	case "played":
		counts.Played++
	default:
		return false
	}
	return true
}

func (a *LocalAnalytics) Record(event, name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !incrementCounts(&a.state.Totals, event) {
		return
	}
	counts := a.state.Sounds[name]
	incrementCounts(&counts, event)
	a.state.Sounds[name] = counts
	a.state.UpdatedAt = time.Now().UTC()
	a.version++
}

func (a *LocalAnalytics) snapshotLocked() AnalyticsSnapshot {
	snapshot := a.state
	snapshot.Sounds = make(map[string]SoundCounts, len(a.state.Sounds))
	for name, counts := range a.state.Sounds {
		snapshot.Sounds[name] = counts
	}
	return snapshot
}

func (a *LocalAnalytics) Snapshot() AnalyticsSnapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.snapshotLocked()
}

func (a *LocalAnalytics) Flush() error {
	a.flushMu.Lock()
	defer a.flushMu.Unlock()
	a.mu.RLock()
	if a.version == a.savedVersion {
		a.mu.RUnlock()
		return nil
	}
	version := a.version
	snapshot := a.snapshotLocked()
	a.mu.RUnlock()
	if err := atomicWriteJSON(a.filename, snapshot); err != nil {
		return err
	}
	a.mu.Lock()
	a.savedVersion = version
	a.mu.Unlock()
	return nil
}

func (a *LocalAnalytics) Close() error {
	a.closeOnce.Do(func() { close(a.stop) })
	<-a.done
	return a.Flush()
}

// Browser reports only sound IDs. Names are resolved against the current board,
// rather than allowing arbitrary names or event types into persistent counters.
func analyticsHandler(a *LocalAnalytics, soundName func(string) (string, bool)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/analytics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if a == nil {
			http.Error(w, "Local analytics unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		encoder.Encode(a.Snapshot())
	})
	mux.HandleFunc("/analytics/play", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if a == nil {
			http.Error(w, "Local analytics unavailable", http.StatusServiceUnavailable)
			return
		}
		input := struct {
			SoundID string `json:"soundID"`
		}{}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input); err != nil {
			http.Error(w, "Invalid playback event", http.StatusBadRequest)
			return
		}
		name, ok := soundName(input.SoundID)
		if !ok {
			http.Error(w, "Unknown sound", http.StatusBadRequest)
			return
		}
		a.Record("played", name)
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}
