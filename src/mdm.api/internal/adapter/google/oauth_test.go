package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := NewClient()
	c.TokenURL = srv.URL + "/token"
	c.RevokeURL = srv.URL + "/revoke"
	c.CalendarAPI = srv.URL + "/calendar/v3"
	return c
}

func TestAuthCodeURL(t *testing.T) {
	c := NewClient()
	u, err := url.Parse(c.AuthCodeURL("cid", "https://mdm.isha.net/cb", "st ate+1"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	want := map[string]string{
		"client_id": "cid", "redirect_uri": "https://mdm.isha.net/cb", "response_type": "code",
		"scope": ReadOnlyCalendarScope, "access_type": "offline", "prompt": "consent", "state": "st ate+1",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if u.Host != "accounts.google.com" {
		t.Errorf("host %q", u.Host)
	}
	// Read-only is the only scope: nothing that could write to a calendar.
	if strings.Contains(q.Get("scope"), " ") || !strings.HasSuffix(q.Get("scope"), "calendar.readonly") {
		t.Errorf("scope must be exactly calendar.readonly, got %q", q.Get("scope"))
	}
}

func TestExchangeCode_SendsCodeFlowFormAndParsesTokens(t *testing.T) {
	var got url.Values
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/token" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("unexpected request %s %s %s", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		r.ParseForm()
		got = r.PostForm
		json.NewEncoder(w).Encode(map[string]any{"access_token": "ya29.A", "refresh_token": "1//R", "expires_in": 3599})
	}))
	tok, err := c.ExchangeCode(context.Background(), "cid", "sec", "the-code", "https://mdm.isha.net/cb")
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "ya29.A" || tok.RefreshToken != "1//R" || tok.ExpiresIn != 3599 {
		t.Errorf("token: %+v", tok)
	}
	for k, v := range map[string]string{"code": "the-code", "client_id": "cid", "client_secret": "sec", "redirect_uri": "https://mdm.isha.net/cb", "grant_type": "authorization_code"} {
		if got.Get(k) != v {
			t.Errorf("form %s = %q, want %q", k, got.Get(k), v)
		}
	}
}

func TestExchangeCode_NoRefreshTokenIsReportedAsEmpty(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"access_token": "ya29.A", "expires_in": 3599})
	}))
	tok, err := c.ExchangeCode(context.Background(), "cid", "sec", "c", "r")
	if err != nil || tok.RefreshToken != "" {
		t.Errorf("want empty refresh token and no error, got %+v, %v", tok, err)
	}
}

func TestRefreshAccessToken(t *testing.T) {
	var got url.Values
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		got = r.PostForm
		json.NewEncoder(w).Encode(map[string]any{"access_token": "ya29.NEW", "expires_in": 3599})
	}))
	at, err := c.RefreshAccessToken(context.Background(), "cid", "sec", "1//R")
	if err != nil || at != "ya29.NEW" {
		t.Fatalf("got %q, %v", at, err)
	}
	if got.Get("grant_type") != "refresh_token" || got.Get("refresh_token") != "1//R" {
		t.Errorf("form: %v", got)
	}
}

func TestErrors_ParsedAndNeverContainSecrets(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "Token has been expired or revoked."})
	}))
	_, err := c.RefreshAccessToken(context.Background(), "cid", "SECRET-VALUE", "REFRESH-VALUE")
	if err == nil || !IsInvalidGrant(err) {
		t.Fatalf("want invalid_grant, got %v", err)
	}
	for _, leak := range []string{"SECRET-VALUE", "REFRESH-VALUE"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error leaks %q: %v", leak, err)
		}
	}

	// Other failure kinds are not mistaken for invalid_grant.
	c2 := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "redirect_uri_mismatch"})
	}))
	_, err = c2.ExchangeCode(context.Background(), "cid", "sec", "c", "r")
	if err == nil || IsInvalidGrant(err) || !strings.Contains(err.Error(), "redirect_uri_mismatch") {
		t.Errorf("got %v", err)
	}
}

func TestTransportErrorsDoNotLeakTheRequestURL(t *testing.T) {
	c := NewClient()
	c.TokenURL = "http://127.0.0.1:1/token?client_secret=LEAK-ME" // nothing listens on port 1
	_, err := c.RefreshAccessToken(context.Background(), "cid", "sec", "r")
	if err == nil || strings.Contains(err.Error(), "LEAK-ME") {
		t.Errorf("transport error must not echo the URL: %v", err)
	}
}

func TestNonJSONErrorBodyStillFails(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		w.Write([]byte("<html>bad gateway</html>"))
	}))
	if _, err := c.RefreshAccessToken(context.Background(), "cid", "sec", "r"); err == nil {
		t.Error("a 502 must be an error")
	}
}

func TestListCalendars_PaginatesAndPrefersUserRename(t *testing.T) {
	var auth []string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		if r.URL.Query().Get("pageToken") == "" {
			json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{"id": "me@mail.isha.org.tw", "summary": "me@mail.isha.org.tw", "accessRole": "owner", "primary": true},
					{"id": "trips@group.calendar.google.com", "summary": "出差行事曆", "summaryOverride": "我的出差", "accessRole": "reader"},
				},
				"nextPageToken": "page2",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "c3", "summary": "Holidays", "accessRole": "reader"}}})
	}))
	cals, err := c.ListCalendars(context.Background(), "ya29.TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if len(cals) != 3 {
		t.Fatalf("want 3 calendars across 2 pages, got %+v", cals)
	}
	if !cals[0].Primary || cals[0].ID != "me@mail.isha.org.tw" {
		t.Errorf("primary: %+v", cals[0])
	}
	if cals[1].Summary != "我的出差" || cals[1].AccessRole != "reader" {
		t.Errorf("rename should win: %+v", cals[1])
	}
	for _, a := range auth {
		if a != "Bearer ya29.TOKEN" {
			t.Errorf("authorization header %q", a)
		}
	}
}

func TestListCalendars_ErrorIsParsed(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": 403, "message": "Google Calendar API has not been used in project 123 before or it is disabled.", "status": "PERMISSION_DENIED"}})
	}))
	_, err := c.ListCalendars(context.Background(), "t")
	if err == nil || !strings.Contains(err.Error(), "PERMISSION_DENIED") || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("got %v", err)
	}
}

func TestRevoke(t *testing.T) {
	var got url.Values
	status := 200
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		got = r.PostForm
		w.WriteHeader(status)
	}))
	if err := c.Revoke(context.Background(), "1//R"); err != nil || got.Get("token") != "1//R" {
		t.Errorf("revoke: %v, form %v", err, got)
	}
	status = 400
	if err := c.Revoke(context.Background(), "1//R"); err == nil {
		t.Error("non-200 should be an error")
	}
}

func TestListEvents_QueryParsingAndFiltering(t *testing.T) {
	var gotPath, gotAuth string
	var gotQuery url.Values
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotQuery = r.URL.EscapedPath(), r.Header.Get("Authorization"), r.URL.Query()
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
			{"id": "e1", "status": "confirmed", "summary": "苗栗出差", "description": "地點： 苗栗、台中<br>交通：高鐵", "location": "苗栗縣",
				"htmlLink": "https://calendar.google.com/e1", "start": map[string]any{"dateTime": "2026-10-20T08:00:00+08:00"}, "end": map[string]any{"dateTime": "2026-10-20T17:00:00+08:00"}},
			{"id": "e2", "status": "confirmed", "summary": "全天", "start": map[string]any{"date": "2026-10-21"}, "end": map[string]any{"date": "2026-10-22"}},
			{"id": "e3", "status": "cancelled", "summary": "取消的"},
		}})
	}))
	min := time.Date(2026, 10, 19, 16, 0, 0, 0, time.FixedZone("TPE", 8*3600)) // 08:00 UTC
	max := min.AddDate(0, 0, 60)
	evs, err := c.ListEvents(context.Background(), "ya29.T", "6fa1@group.calendar.google.com", min, max)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/calendar/v3/calendars/6fa1@group.calendar.google.com/events" && gotPath != "/calendar/v3/calendars/6fa1%40group.calendar.google.com/events" {
		t.Errorf("path %q", gotPath)
	}
	if gotAuth != "Bearer ya29.T" || gotQuery.Get("singleEvents") != "true" || gotQuery.Get("orderBy") != "startTime" {
		t.Errorf("auth %q query %v", gotAuth, gotQuery)
	}
	if gotQuery.Get("timeMin") != "2026-10-19T08:00:00Z" {
		t.Errorf("timeMin must be sent as UTC, got %q", gotQuery.Get("timeMin"))
	}
	if len(evs) != 2 {
		t.Fatalf("cancelled events must be dropped; got %+v", evs)
	}
	if evs[0].Start != "2026-10-20T08:00:00+08:00" || evs[0].AllDay || evs[0].Location != "苗栗縣" || evs[0].Description == "" {
		t.Errorf("timed event: %+v", evs[0])
	}
	if !evs[1].AllDay || evs[1].Start != "2026-10-21" || evs[1].End != "2026-10-22" {
		t.Errorf("all-day event: %+v", evs[1])
	}
}

func TestListEvents_EscapesCalendarIDInThePath(t *testing.T) {
	var raw string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.EscapedPath()
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	// An id containing path characters must not be able to reach another endpoint.
	if _, err := c.ListEvents(context.Background(), "t", "x/../../users/me/calendarList?", time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "/../") || strings.HasSuffix(raw, "calendarList") {
		t.Errorf("calendar id was not escaped: %q", raw)
	}
}

func TestListEvents_Paginates(t *testing.T) {
	calls := 0
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("pageToken") == "" {
			json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "a", "start": map[string]any{"date": "2026-10-20"}}}, "nextPageToken": "p2"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "b", "start": map[string]any{"date": "2026-10-21"}}}})
	}))
	evs, err := c.ListEvents(context.Background(), "t", "cal", time.Now(), time.Now())
	if err != nil || len(evs) != 2 || calls != 2 || evs[1].ID != "b" {
		t.Errorf("events %+v calls %d err %v", evs, calls, err)
	}
}

func TestListEvents_PaginationIsBounded(t *testing.T) {
	calls := 0
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "nextPageToken": "again"}) // never ends
	}))
	if _, err := c.ListEvents(context.Background(), "t", "cal", time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if calls != 8 {
		t.Errorf("should stop after 8 pages, made %d requests", calls)
	}
}

func TestListEvents_NotFoundIsParsed(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": 404, "message": "Not Found", "status": "NOT_FOUND"}})
	}))
	_, err := c.ListEvents(context.Background(), "t", "nope", time.Now(), time.Now())
	if err == nil || !strings.Contains(err.Error(), "NOT_FOUND") {
		t.Errorf("got %v", err)
	}
}
