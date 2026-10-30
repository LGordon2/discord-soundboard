package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAnalyticsHourlyPersistenceAndRetry(t *testing.T) {
	db := testDatabase(t, filepath.Join(t.TempDir(), "history.sqlite3"))
	a, err := newLocalAnalytics(db)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	at := time.Date(2026, 10, 30, 23, 45, 0, 0, time.FixedZone("offset", -4*3600))
	a.recordAt("played", "One", at)
	a.recordAt("sent", "Two", at.Add(20*time.Minute))
	a.recordAt("added", "One", at)
	a.recordAt("invalid", "One", at)
	snapshot := a.Snapshot()
	if len(snapshot.Hourly) != 2 || snapshot.Hourly[0].At.Hour() != 3 || snapshot.Hourly[0].Played != 1 || snapshot.Hourly[0].Added != 1 || snapshot.Hourly[1].Sent != 1 {
		t.Fatalf("incorrect UTC buckets: %+v", snapshot.Hourly)
	}
	snapshot.Hourly[0].Played = 900
	if a.Snapshot().Hourly[0].Played != 1 {
		t.Fatal("snapshot modified live buckets")
	}
	db.Exec("PRAGMA query_only=ON")
	if err := a.Flush(); err == nil {
		t.Fatal("expected failure")
	}
	db.Exec("PRAGMA query_only=OFF")
	if err := a.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := a.Flush(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newLocalAnalytics(db)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	saved := reopened.Snapshot()
	if len(saved.Hourly) != 2 || saved.Hourly[0].Played != 1 || !saved.HistorySince.Equal(snapshot.HistorySince) {
		t.Fatalf("lost/doubled history: %+v", saved)
	}
}

func TestAnalyticsTimelineAggregationAndCoverage(t *testing.T) {
	now := time.Date(2026, 10, 30, 15, 30, 0, 0, time.UTC)
	snapshot := AnalyticsSnapshot{HistorySince: now.Add(-2 * time.Hour), Totals: SoundCounts{Played: 100}, Hourly: []AnalyticsBucket{
		{At: now.Add(-2 * time.Hour).Truncate(time.Hour), SoundCounts: SoundCounts{Played: 2}},
		{At: now.Add(-time.Hour).Truncate(time.Hour), SoundCounts: SoundCounts{Sent: 3}},
	}}
	hourly := buildAnalyticsTimeline(snapshot, "24h", now)
	if len(hourly.Series[0].Dots) != 3 || hourly.Series[0].Dots[0].Value != 2 || hourly.Series[0].Dots[2].Value != 0 || !hourly.HasEvents {
		t.Fatalf("bad hourly chart: %+v", hourly)
	}
	daily := buildAnalyticsTimeline(snapshot, "30d", now)
	if len(daily.Series[0].Dots) != 1 || daily.Series[0].Dots[0].Value != 2 || daily.Series[1].Dots[0].Value != 3 {
		t.Fatal("daily rollup wrong or old totals backfilled")
	}
	snapshot.HistorySince = now.Add(-100 * 24 * time.Hour)
	longest := buildAnalyticsTimeline(snapshot, "90d", now)
	if len(longest.Series[0].Dots) != 90 {
		t.Fatal("range not bounded")
	}
	fallback := buildAnalyticsTimeline(snapshot, "unexpected", now)
	if fallback.Range != "7d" || len(fallback.Series[0].Dots) != 7 {
		t.Fatal("invalid range not normalized")
	}
}

func TestTimeHistoryMigrationDoesNotBackfillTotals(t *testing.T) {
	db := testDatabase(t, filepath.Join(t.TempDir(), "migration.sqlite3"))
	for _, statement := range []string{"DROP TABLE analytics_hourly", "DELETE FROM app_settings WHERE key='analytics_history_since'", "PRAGMA user_version=2", "UPDATE analytics_state SET played=42"} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := initializeDatabase(db, "", ""); err != nil {
		t.Fatal(err)
	}
	a, err := newLocalAnalytics(db)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	snapshot := a.Snapshot()
	if snapshot.Totals.Played != 42 || len(snapshot.Hourly) != 0 || snapshot.HistorySince.IsZero() {
		t.Fatal("migration lost totals or fabricated history")
	}
	var version int
	db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != 3 {
		t.Fatal("schema not upgraded")
	}
}
