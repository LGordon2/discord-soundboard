package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestAnalyticsPersistenceAndConcurrentRecording(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "analytics.sqlite3")
	db := testDatabase(t, filename)
	a, err := newLocalAnalytics(db)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 50; j++ {
				a.Record("added", "one")
				a.Record("sent", "one")
				a.Record("played", "two")
			}
		}()
	}
	workers.Wait()
	a.Record("invalid", "ignored")
	snapshot := a.Snapshot()
	want := SoundCounts{Added: 1000, Sent: 1000, Played: 1000}
	if snapshot.Totals != want || snapshot.Sounds["one"] != (SoundCounts{Added: 1000, Sent: 1000}) || snapshot.Sounds["two"].Played != 1000 {
		t.Fatalf("incorrect counts: %+v", snapshot)
	}
	snapshot.Sounds["one"] = SoundCounts{}
	if a.Snapshot().Sounds["one"].Added != 1000 {
		t.Fatal("snapshot mutated live counters")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filename)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("file permissions: %v %v", info, err)
	}
	reopened, err := newLocalAnalytics(testDatabase(t, filename))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Snapshot().Totals != want || !reopened.Snapshot().Since.Equal(snapshot.Since) {
		t.Fatalf("restart lost counters: %+v", reopened.Snapshot())
	}
	reopened.Record("sent", "one")
	if err := reopened.Flush(); err != nil {
		t.Fatal(err)
	}
	var sent uint64
	if err := db.QueryRow("SELECT sent FROM sound_counts WHERE name='one'").Scan(&sent); err != nil || sent != 1001 {
		t.Fatalf("saved sent=%d err=%v", sent, err)
	}
}

func TestAnalyticsRetriesFailedTransactions(t *testing.T) {
	db := testDatabase(t, filepath.Join(t.TempDir(), "test.sqlite3"))
	a, err := newLocalAnalytics(db)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Record("sent", "sound")
	if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	if err := a.Flush(); err == nil {
		t.Fatal("expected write failure")
	}
	if a.Snapshot().Totals.Sent != 1 {
		t.Fatal("write failure discarded memory state")
	}
	if _, err := db.Exec("PRAGMA query_only=OFF"); err != nil {
		t.Fatal(err)
	}
	if err := a.Flush(); err != nil {
		t.Fatal(err)
	}
	var sent uint64
	if err := db.QueryRow("SELECT sent FROM sound_counts WHERE name='sound'").Scan(&sent); err != nil || sent != 1 {
		t.Fatalf("saved sent=%d err=%v", sent, err)
	}
}

func TestAnalyticsHTTP(t *testing.T) {
	a, err := newLocalAnalytics(testDatabase(t, filepath.Join(t.TempDir(), "analytics.sqlite3")))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	handler := analyticsHandler(a, func(id string) (string, bool) { return "Sound & Name", id == "known" })
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/analytics/play", `{"soundID":"known"}`, 204},
		{"POST", "/analytics/play", `{"soundID":"unknown"}`, 400},
		{"POST", "/analytics/play", `{`, 400},
		{"POST", "/analytics/play", strings.Repeat("x", 2048), 400},
		{"GET", "/analytics/play", "", 405},
		{"POST", "/analytics/data", "", 405},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		if response.Code != tc.status {
			t.Errorf("%s %s: %d, want %d", tc.method, tc.path, response.Code, tc.status)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/analytics/data", nil))
	var snapshot AnalyticsSnapshot
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if response.Header().Get("Cache-Control") != "no-store" || snapshot.Totals != (SoundCounts{Played: 1}) || snapshot.Sounds["Sound & Name"].Played != 1 {
		t.Fatalf("incorrect response: %s", response.Body.String())
	}
	disabled := httptest.NewRecorder()
	analyticsHandler(nil, nil).ServeHTTP(disabled, httptest.NewRequest("GET", "/analytics/data", nil))
	if disabled.Code != 503 {
		t.Fatalf("disabled status %d", disabled.Code)
	}
}

func TestDiscordAnalyticsCountSuccessOnceAfterRetry(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			calls, added, sent := 0, 0, 0
			client := testDiscordClient(t, func(r *http.Request) (*http.Response, error) {
				calls++
				status, body := 201, `{"sound_id":"created"}`
				if !success {
					status, body = 403, `{"message":"forbidden"}`
				} else if calls == 1 {
					status, body = 429, `{"retry_after":0.001,"global":false}`
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			client.onSoundCreated = func(name string) {
				added++
				if name != "name" {
					t.Errorf("name=%s", name)
				}
			}
			client.onSoundSent = func(id string) {
				sent++
				if id != "created" {
					t.Errorf("id=%s", id)
				}
			}
			_, err := client.CreateSoundboardSound(guildID, "name", "audio/ogg", []byte("audio"))
			if (err == nil) != success {
				t.Fatalf("create error %v", err)
			}
			err = client.SendSoundboardSound(guildID, channelID, "created")
			if (err == nil) != success {
				t.Fatalf("send error %v", err)
			}
			expected := 0
			if success {
				expected = 1
			}
			if added != expected || sent != expected {
				t.Fatalf("added=%d sent=%d", added, sent)
			}
		})
	}
}

func TestAnalyticsBatchRollsBackAndRetriesAsOneTransaction(t *testing.T) {
	db := testDatabase(t, filepath.Join(t.TempDir(), "test.sqlite3"))
	a, err := newLocalAnalytics(db)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := db.Exec("CREATE TRIGGER reject_counts BEFORE INSERT ON sound_counts BEGIN SELECT RAISE(ABORT,'test failure'); END"); err != nil {
		t.Fatal(err)
	}
	a.Record("sent", "new sound")
	if err := a.Flush(); err == nil {
		t.Fatal("expected row write failure")
	}
	var saved uint64
	if err := db.QueryRow("SELECT sent FROM analytics_state WHERE id=1").Scan(&saved); err != nil || saved != 0 {
		t.Fatalf("partial transaction committed: %d %v", saved, err)
	}
	if _, err := db.Exec("DROP TRIGGER reject_counts"); err != nil {
		t.Fatal(err)
	}
	if err := a.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT sent FROM analytics_state WHERE id=1").Scan(&saved); err != nil || saved != 1 {
		t.Fatalf("retry lost count: %d %v", saved, err)
	}
}
