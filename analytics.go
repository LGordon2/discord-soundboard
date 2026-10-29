package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
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

// Record only touches memory; changed rows are committed in a background transaction.
type LocalAnalytics struct {
	mu           sync.RWMutex
	flushMu      sync.Mutex
	state        AnalyticsSnapshot
	version      uint64
	savedVersion uint64
	db           *sql.DB
	dirty        map[string]uint64
	stop         chan struct{}
	done         chan struct{}
	closeOnce    sync.Once
}

func newLocalAnalytics(db *sql.DB) (*LocalAnalytics, error) {
	a := &LocalAnalytics{db: db, state: AnalyticsSnapshot{Version: 1, Sounds: make(map[string]SoundCounts)}, dirty: make(map[string]uint64), stop: make(chan struct{}), done: make(chan struct{})}
	var since, updated string
	if err := db.QueryRow("SELECT since,updated_at,added,sent,played FROM analytics_state WHERE id=1").Scan(&since, &updated, &a.state.Totals.Added, &a.state.Totals.Sent, &a.state.Totals.Played); err != nil {
		return nil, err
	}
	var err error
	if a.state.Since, err = time.Parse(time.RFC3339Nano, since); err != nil {
		return nil, err
	}
	if a.state.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return nil, err
	}
	rows, err := db.Query("SELECT name,added,sent,played FROM sound_counts")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var counts SoundCounts
		if err := rows.Scan(&name, &counts.Added, &counts.Sent, &counts.Played); err != nil {
			return nil, err
		}
		a.state.Sounds[name] = counts
	}
	if err := rows.Err(); err != nil {
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
	a.dirty[name] = a.version
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
	state := a.state
	changed := make(map[string]SoundCounts, len(a.dirty))
	for name := range a.dirty {
		changed[name] = a.state.Sounds[name]
	}
	a.mu.RUnlock()
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE analytics_state SET updated_at=?,added=?,sent=?,played=? WHERE id=1", state.UpdatedAt.Format(time.RFC3339Nano), state.Totals.Added, state.Totals.Sent, state.Totals.Played); err != nil {
		return err
	}
	statement, err := tx.Prepare("INSERT INTO sound_counts(name,added,sent,played) VALUES(?,?,?,?) ON CONFLICT(name) DO UPDATE SET added=excluded.added,sent=excluded.sent,played=excluded.played")
	if err != nil {
		return err
	}
	defer statement.Close()
	for name, counts := range changed {
		if _, err = statement.Exec(name, counts.Added, counts.Sent, counts.Played); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	a.mu.Lock()
	a.savedVersion = version
	for name, revision := range a.dirty {
		if revision <= version {
			delete(a.dirty, name)
		}
	}
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
