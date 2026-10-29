package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testDatabase(t *testing.T, filename string) *sql.DB {
	t.Helper()
	db, err := openSoundboardDatabase(filename, "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestLegacyJSONMigrationIsAtomicAndIdempotent(t *testing.T) {
	directory := t.TempDir()
	analyticsFile := filepath.Join(directory, "analytics.json")
	metadataFile := filepath.Join(directory, "sound-metadata.json")
	databaseFile := filepath.Join(directory, "soundboard.sqlite3")
	since := time.Now().UTC().Truncate(time.Second)
	analytics := AnalyticsSnapshot{Version: 1, Since: since, UpdatedAt: since, Totals: SoundCounts{Added: 7, Sent: 8, Played: 9}, Sounds: map[string]SoundCounts{"quote": {Added: 7, Sent: 8, Played: 9}}}
	metadata := soundMetadataFile{Version: 1, Sounds: map[string]SoundMetadata{"quote": {Description: "From a movie", EmojiName: "🎬"}, "custom": {EmojiID: "12345"}}}
	data, _ := json.Marshal(analytics)
	if err := os.WriteFile(analyticsFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	originalAnalytics := string(data)
	if err := os.WriteFile(metadataFile, []byte("bad json"), 0600); err != nil {
		t.Fatal(err)
	}
	if db, err := openSoundboardDatabase(databaseFile, analyticsFile, metadataFile); err == nil {
		db.Close()
		t.Fatal("corrupt migration succeeded")
	}
	data, _ = json.Marshal(metadata)
	if err := os.WriteFile(metadataFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := openSoundboardDatabase(databaseFile, analyticsFile, metadataFile)
	if err != nil {
		t.Fatal(err)
	}
	a, err := newLocalAnalytics(db)
	if err != nil {
		t.Fatal(err)
	}
	if a.Snapshot().Totals != analytics.Totals || !a.Snapshot().Since.Equal(since) {
		t.Fatalf("wrong imported analytics: %+v", a.Snapshot())
	}
	a.Record("sent", "quote")
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	details, err := newSoundMetadataStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if details.Snapshot()["quote"] != metadata.Sounds["quote"] || details.Snapshot()["custom"].EmojiID != "12345" {
		t.Fatalf("wrong metadata: %+v", details.Snapshot())
	}
	details.Close()
	db.Close()
	// Legacy files stay in place. A restart must not overwrite newer DB values.
	db, err = openSoundboardDatabase(databaseFile, analyticsFile, metadataFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, err = newLocalAnalytics(db)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Snapshot().Totals.Sent != 9 || a.Snapshot().Sounds["quote"].Sent != 9 {
		t.Fatal("migration ran twice or overwrote new counts")
	}
	original, _ := os.ReadFile(analyticsFile)
	if string(original) != originalAnalytics {
		t.Fatal("legacy file modified")
	}
	var result string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil || result != "ok" {
		t.Fatalf("integrity: %s %v", result, err)
	}
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&result); err != nil || result != "wal" {
		t.Fatalf("journal: %s %v", result, err)
	}
}

func TestDatabaseRejectsFutureSchema(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "future.sqlite3")
	db := testDatabase(t, filename)
	if _, err := db.Exec("PRAGMA user_version=99"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if db, err := openSoundboardDatabase(filename, "", ""); err == nil {
		db.Close()
		t.Fatal("accepted future schema")
	}
}
