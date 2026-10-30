package main

import (
	"html/template"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

var analyticsPageTemplate = template.Must(template.ParseFS(adminAssets, "admin.html", "analytics.html"))

type analyticsPageHandler struct {
	auth      *uploadAuth
	analytics *LocalAnalytics
}
type analyticsRow struct {
	Name                string
	Added, Sent, Played uint64
}
type analyticsBar struct {
	Name  string
	Count uint64
	Width float64
}
type analyticsChart struct {
	Title, Metric, Description string
	Bars                       []analyticsBar
}
type analyticsPageData struct {
	SignedIn, Admin, CanView   bool
	Username, Since, UpdatedAt string
	Totals                     SoundCounts
	Rows                       []analyticsRow
	Charts                     []analyticsChart
	Timeline                   analyticsTimeline
}

func buildAnalyticsPage(snapshot AnalyticsSnapshot) analyticsPageData {
	page := analyticsPageData{Totals: snapshot.Totals, Since: snapshot.Since.UTC().Format("Jan 2, 2006, 15:04 UTC"), UpdatedAt: snapshot.UpdatedAt.UTC().Format("Jan 2, 2006, 15:04:05 UTC")}
	for name, counts := range snapshot.Sounds {
		page.Rows = append(page.Rows, analyticsRow{Name: name, Added: counts.Added, Sent: counts.Sent, Played: counts.Played})
	}
	sort.Slice(page.Rows, func(i, j int) bool {
		if page.Rows[i].Played != page.Rows[j].Played {
			return page.Rows[i].Played > page.Rows[j].Played
		}
		return analyticsNameLess(page.Rows[i].Name, page.Rows[j].Name)
	})
	for _, spec := range []struct {
		title, metric, description string
		value                      func(analyticsRow) uint64
	}{
		{"Most played", "played", "Browser playback events, including previews.", func(r analyticsRow) uint64 { return r.Played }},
		{"Most sent", "sent", "Successful sends to the Discord voice channel.", func(r analyticsRow) uint64 { return r.Sent }},
		{"Most added", "added", "Successful additions to the Discord soundboard.", func(r analyticsRow) uint64 { return r.Added }},
	} {
		chart := analyticsChart{Title: spec.title, Metric: spec.metric, Description: spec.description}
		sorted := append([]analyticsRow(nil), page.Rows...)
		sort.Slice(sorted, func(i, j int) bool {
			left, right := spec.value(sorted[i]), spec.value(sorted[j])
			if left != right {
				return left > right
			}
			return analyticsNameLess(sorted[i].Name, sorted[j].Name)
		})
		for _, row := range sorted {
			count := spec.value(row)
			if count == 0 || len(chart.Bars) == 8 {
				break
			}
			chart.Bars = append(chart.Bars, analyticsBar{Name: row.Name, Count: count, Width: float64(count) / float64(spec.value(sorted[0])) * 100})
		}
		page.Charts = append(page.Charts, chart)
	}
	return page
}

func (h *analyticsPageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	if r.URL.Path != "/analytics" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(405)
		return
	}
	session, err := h.auth.session(r)
	if err != nil {
		http.Error(w, "Could not load Discord login", 503)
		return
	}
	page := analyticsPageData{}
	if session != nil {
		page.SignedIn = true
		page.Username = session.Username
		page.Admin = h.auth.configured() && session.ID == adminDiscordID
		page.CanView = h.auth.configured() && h.auth.allowed(session.ID)
	}
	if page.CanView {
		if h.analytics == nil {
			http.Error(w, "Analytics unavailable", 503)
			return
		}
		snapshot := h.analytics.Snapshot()
		data := buildAnalyticsPage(snapshot)
		data.Timeline = buildAnalyticsTimeline(snapshot, r.URL.Query().Get("range"), time.Now().UTC())
		data.Admin = page.Admin
		data.CanView = true
		data.SignedIn = true
		data.Username = page.Username
		page = data
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := analyticsPageTemplate.ExecuteTemplate(w, "analytics.html", page); err != nil {
		log.Printf("[analytics] rendering charts: %v", err)
	}
}

// Keep chart labels stable when names differ only in capitalization.
func analyticsNameLess(a, b string) bool {
	left, right := strings.ToLower(a), strings.ToLower(b)
	if left == right {
		return a < b
	}
	return left < right
}

// The chart data follows the same access rules as the charts page.
func analyticsDataHandler(auth *uploadAuth, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		session, err := auth.session(r)
		if err != nil {
			http.Error(w, "Could not load Discord login", 503)
			return
		}
		if session == nil {
			http.Error(w, "Sign in with Discord to view analytics", 401)
			return
		}
		if !auth.configured() || !auth.allowed(session.ID) {
			http.Error(w, "Analytics access required", 403)
			return
		}
		next.ServeHTTP(w, r)
	})
}
