package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAnalyticsChartRankingsAndEmptyData(t *testing.T) {
	snapshot := AnalyticsSnapshot{Since: time.Now(), UpdatedAt: time.Now(), Sounds: map[string]SoundCounts{"Beta": {Played: 5, Sent: 8}, "Alpha": {Played: 10, Added: 2}, "Zero": {}, "Another": {Played: 5}}}
	page := buildAnalyticsPage(snapshot)
	if len(page.Charts) != 3 || page.Rows[0].Name != "Alpha" {
		t.Fatalf("incorrect rankings: %+v", page)
	}
	played, sent, added := page.Charts[0], page.Charts[1], page.Charts[2]
	if len(played.Bars) != 3 || played.Bars[0].Width != 100 || played.Bars[1].Width != 50 || played.Bars[1].Name != "Another" {
		t.Fatalf("wrong play chart: %+v", played)
	}
	if len(sent.Bars) != 1 || sent.Bars[0].Name != "Beta" || sent.Bars[0].Count != 8 {
		t.Fatalf("wrong send chart: %+v", sent)
	}
	if len(added.Bars) != 1 || added.Bars[0].Name != "Alpha" {
		t.Fatalf("wrong additions chart: %+v", added)
	}
	for i := 0; i < 12; i++ {
		snapshot.Sounds[strings.Repeat("x", i+1)] = SoundCounts{Played: 1}
	}
	page = buildAnalyticsPage(snapshot)
	if len(page.Charts[0].Bars) != 8 {
		t.Fatal("chart should cap at eight sounds")
	}
	empty := buildAnalyticsPage(AnalyticsSnapshot{})
	for _, chart := range empty.Charts {
		if len(chart.Bars) != 0 {
			t.Fatal("empty chart has bars")
		}
	}
}

func TestAnalyticsPageAuthenticationAndRendering(t *testing.T) {
	admin, cookie, _, _ := testAdmin(t)
	analytics, err := newLocalAnalytics(admin.auth.db)
	if err != nil {
		t.Fatal(err)
	}
	defer analytics.Close()
	name := `<script>alert("x")</script>`
	admin.auth.allowedIDs = adminDiscordID + ",12345"
	analytics.Record("played", name)
	analytics.Record("sent", "Other")
	h := &analyticsPageHandler{auth: admin.auth, analytics: analytics}
	get := func(authenticated bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/analytics", nil)
		if authenticated {
			req.AddCookie(cookie)
		}
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		return out
	}
	out := get(false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), "Sign in to view analytics") || strings.Contains(out.Body.String(), `class="bar-track"`) {
		t.Fatal("logged-out chart data exposed")
	}
	if !strings.Contains(out.Body.String(), "next=/analytics") {
		t.Fatal("analytics login return missing")
	}
	out = get(true)
	body := out.Body.String()
	if out.Code != 200 || !strings.Contains(body, `width:100.0000%`) || strings.Contains(body, "ZgotmplZ") || strings.Contains(body, name) || !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("incorrect or unsafe analytics rendering: %d", out.Code)
	}
	if !strings.Contains(body, "No events recorded yet.") || out.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing empty state or caching protection")
	}
	if _, err := admin.auth.db.Exec("UPDATE upload_sessions SET user_id='12345',username='approved-user'"); err != nil {
		t.Fatal(err)
	}
	out = get(true)
	if !strings.Contains(out.Body.String(), `class="bar-track"`) || strings.Contains(out.Body.String(), ">Manage sounds</a>") {
		t.Fatal("approved uploader cannot view analytics or received admin navigation")
	}
	admin.auth.allowedIDs = adminDiscordID // Revocation applies to existing sessions.
	out = get(true)
	if strings.Contains(out.Body.String(), `class="bar-track"`) || strings.Contains(out.Body.String(), "&lt;script&gt;") {
		t.Fatal("non-allowlisted account received analytics data")
	}
	if _, err := admin.auth.db.Exec("UPDATE upload_sessions SET user_id=?,expires=0", adminDiscordID); err != nil {
		t.Fatal(err)
	}
	out = get(true)
	if strings.Contains(out.Body.String(), `class="bar-track"`) {
		t.Fatal("expired session received analytics data")
	}

	req := httptest.NewRequest("POST", "/analytics", nil)
	req.AddCookie(cookie)
	out = httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 405 {
		t.Fatal("analytics page accepts mutation")
	}
}

func TestAnalyticsOAuthReturnAndRedirectAllowlist(t *testing.T) {
	for _, next := range []string{"/", "/analytics", "https://example.com", "//example.com"} {
		auth, _ := testUploadAuth(t)
		begin := httptest.NewRecorder()
		auth.login(begin, httptest.NewRequest("GET", "/auth/discord?next="+next, nil))
		req := httptest.NewRequest("GET", "/auth/discord/callback?code=test-code&state="+begin.Result().Cookies()[0].Value, nil)
		for _, cookie := range begin.Result().Cookies() {
			req.AddCookie(cookie)
		}
		result := httptest.NewRecorder()
		auth.callback(result, req)
		want := "/?upload_login=success"
		if next == "/analytics" || next == "/" {
			want = next
		}
		if result.Code != 303 || result.Header().Get("Location") != want {
			t.Fatalf("unsafe or incorrect return for %s: %s", next, result.Header().Get("Location"))
		}
	}
}

func TestAnalyticsDataUsesUploadAllowlist(t *testing.T) {
	admin, cookie, _, _ := testAdmin(t)
	if _, err := admin.auth.db.Exec("UPDATE upload_sessions SET user_id='12345'"); err != nil {
		t.Fatal(err)
	}
	h := analyticsDataHandler(admin.auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("allowed")) }))
	check := func(loggedIn bool, want int) {
		t.Helper()
		req := httptest.NewRequest("GET", "/analytics/data", nil)
		if loggedIn {
			req.AddCookie(cookie)
		}
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		if out.Code != want {
			t.Fatalf("data access: got %d want %d", out.Code, want)
		}
	}
	check(false, 401)
	check(true, 200)
	admin.auth.allowedIDs = adminDiscordID
	check(true, 403)
}
