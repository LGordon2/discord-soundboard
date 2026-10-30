package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const databaseSchema = `
CREATE TABLE IF NOT EXISTS app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS analytics_state (
 id INTEGER PRIMARY KEY CHECK(id=1), since TEXT NOT NULL, updated_at TEXT NOT NULL,
 added INTEGER NOT NULL DEFAULT 0 CHECK(added>=0), sent INTEGER NOT NULL DEFAULT 0 CHECK(sent>=0), played INTEGER NOT NULL DEFAULT 0 CHECK(played>=0));
CREATE TABLE IF NOT EXISTS sound_counts (
 name TEXT PRIMARY KEY, added INTEGER NOT NULL DEFAULT 0 CHECK(added>=0),
 sent INTEGER NOT NULL DEFAULT 0 CHECK(sent>=0), played INTEGER NOT NULL DEFAULT 0 CHECK(played>=0));
CREATE TABLE IF NOT EXISTS upload_sessions (
 token_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL, username TEXT NOT NULL, csrf TEXT NOT NULL, expires INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS sound_uploads (
 filename TEXT PRIMARY KEY, uploader_id TEXT NOT NULL, uploader_name TEXT NOT NULL, uploaded_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sound_emoji_overrides (name TEXT PRIMARY KEY);
CREATE TABLE IF NOT EXISTS sound_metadata (
 name TEXT PRIMARY KEY, description TEXT NOT NULL DEFAULT '', emoji_id TEXT NOT NULL DEFAULT '', emoji_name TEXT NOT NULL DEFAULT '');
`

func openSoundboardDatabase(filename, legacyAnalytics, legacyMetadata string) (*sql.DB, error) {
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(absolute, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	location := url.URL{Scheme: "file", Path: absolute}
	options := url.Values{"_busy_timeout": {"5000"}, "_journal_mode": {"WAL"}, "_synchronous": {"FULL"}, "_foreign_keys": {"on"}, "_txlock": {"immediate"}}
	location.RawQuery = options.Encode()
	db, err := sql.Open("sqlite3", location.String())
	if err != nil {
		return nil, err
	}
	// This service batches writes; a single connection serializes its transactions.
	// WAL permits concurrent external readers (e.g. sqlite3 inspection/backups).
	db.SetMaxOpenConns(1)
	if err := initializeDatabase(db, legacyAnalytics, legacyMetadata); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func initializeDatabase(db *sql.DB, analyticsFile, metadataFile string) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > 1 {
		return fmt.Errorf("database schema %d is newer than this application", version)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(databaseSchema); err != nil {
		return err
	}
	var imported string
	err = tx.QueryRow("SELECT value FROM app_settings WHERE key='legacy_json_imported'").Scan(&imported)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == sql.ErrNoRows {
		now := time.Now().UTC()
		analytics := AnalyticsSnapshot{Version: 1, Since: now, UpdatedAt: now}
		metadata := soundMetadataFile{Version: 1}
		if found, err := readLegacyJSON(analyticsFile, &analytics); err != nil {
			return fmt.Errorf("import analytics: %w", err)
		} else if found && (analytics.Version != 1 || analytics.Since.IsZero()) {
			return fmt.Errorf("invalid legacy analytics format")
		}
		if found, err := readLegacyJSON(metadataFile, &metadata); err != nil {
			return fmt.Errorf("import sound metadata: %w", err)
		} else if found && metadata.Version != 1 {
			return fmt.Errorf("invalid legacy metadata format")
		}
		if _, err = tx.Exec("INSERT INTO analytics_state(id,since,updated_at,added,sent,played) VALUES(1,?,?,?,?,?)", analytics.Since.Format(time.RFC3339Nano), analytics.UpdatedAt.Format(time.RFC3339Nano), analytics.Totals.Added, analytics.Totals.Sent, analytics.Totals.Played); err != nil {
			return err
		}
		for name, counts := range analytics.Sounds {
			if _, err = tx.Exec("INSERT INTO sound_counts(name,added,sent,played) VALUES(?,?,?,?)", name, counts.Added, counts.Sent, counts.Played); err != nil {
				return err
			}
		}
		for name, entry := range metadata.Sounds {
			if _, err = tx.Exec("INSERT INTO sound_metadata(name,description,emoji_id,emoji_name) VALUES(?,?,?,?)", name, entry.Description, entry.EmojiID, entry.EmojiName); err != nil {
				return err
			}
		}
		if _, err = tx.Exec("INSERT INTO app_settings(key,value) VALUES('legacy_json_imported','1')"); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("PRAGMA user_version=1"); err != nil {
		return err
	}
	return tx.Commit()
}

func readLegacyJSON(filename string, destination any) (bool, error) {
	if filename == "" {
		return false, nil
	}
	data, err := os.ReadFile(filename)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(data, destination)
}
