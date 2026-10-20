package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/mdm-server/internal/adapter/google"
	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/service/tripcar"
)

const testJWTSecret = "test-jwt-secret"

// fakeGoogleRepo is an in-memory stand-in holding exactly what the real table
// would: ciphertext only, so tests can assert on what actually gets stored.
type fakeGoogleRepo struct{ s domain.GoogleCalendarSettings }

func (f *fakeGoogleRepo) Get(context.Context) (*domain.GoogleCalendarSettings, error) {
	c := f.s
	return &c, nil
}
func (f *fakeGoogleRepo) SaveSelectedCalendars(_ context.Context, ids []string, by string) error {
	f.s.SelectedCalendarIDs, f.s.UpdatedBy = ids, &by
	return nil
}
func (f *fakeGoogleRepo) SaveClient(_ context.Context, id, enc string, uris []string, by string) error {
	f.s.ClientID, f.s.ClientSecretEnc, f.s.RedirectURIs = id, enc, uris
	f.s.RefreshTokenEnc, f.s.ConnectedAccount, f.s.SelectedCalendarIDs = "", "", nil
	f.s.UpdatedAt, f.s.UpdatedBy = time.Now(), &by
	return nil
}
func (f *fakeGoogleRepo) SaveRefreshToken(_ context.Context, enc, account, by string) error {
	f.s.RefreshTokenEnc, f.s.ConnectedAccount, f.s.UpdatedBy = enc, account, &by
	return nil
}
func (f *fakeGoogleRepo) ClearRefreshToken(_ context.Context, by string) error {
	f.s.RefreshTokenEnc, f.s.ConnectedAccount, f.s.UpdatedBy = "", "", &by
	return nil
}
func (f *fakeGoogleRepo) Clear(_ context.Context, by string) error {
	f.s = domain.GoogleCalendarSettings{UpdatedAt: time.Now(), UpdatedBy: &by}
	return nil
}

// fakeGoogle records what the controller sends to Google and returns canned answers.
type fakeGoogle struct {
	exchangeToken   *google.Token
	exchangeErr     error
	refreshErr      error
	calendars       []google.Calendar
	listErr         error
	revokeErr       error
	gotClientID     string
	gotClientSecret string
	gotCode         string
	gotRedirectURI  string
	gotRefresh      string
	listedWith      string
	revoked         []string
	events          map[string][]google.Event // by calendar id
	eventsErr       map[string]error
	eventCalls      []string
	eventsFrom      time.Time
	eventsTo        time.Time
}

func (f *fakeGoogle) AuthCodeURL(clientID, redirectURI, state string) string {
	return "https://accounts.example/auth?" + url.Values{"client_id": {clientID}, "redirect_uri": {redirectURI}, "state": {state}}.Encode()
}
func (f *fakeGoogle) ExchangeCode(_ context.Context, id, secret, code, ru string) (*google.Token, error) {
	f.gotClientID, f.gotClientSecret, f.gotCode, f.gotRedirectURI = id, secret, code, ru
	return f.exchangeToken, f.exchangeErr
}
func (f *fakeGoogle) RefreshAccessToken(_ context.Context, id, secret, refresh string) (string, error) {
	f.gotClientID, f.gotClientSecret, f.gotRefresh = id, secret, refresh
	if f.refreshErr != nil {
		return "", f.refreshErr
	}
	return "ya29.ACCESS", nil
}
func (f *fakeGoogle) ListCalendars(_ context.Context, access string) ([]google.Calendar, error) {
	f.listedWith = access
	return f.calendars, f.listErr
}
func (f *fakeGoogle) ListEvents(_ context.Context, access, calendarID string, from, to time.Time) ([]google.Event, error) {
	f.listedWith = access
	f.eventCalls = append(f.eventCalls, calendarID)
	f.eventsFrom, f.eventsTo = from, to
	return f.events[calendarID], f.eventsErr[calendarID]
}
func (f *fakeGoogle) Revoke(_ context.Context, token string) error {
	f.revoked = append(f.revoked, token)
	return f.revokeErr
}

// fakeDirectory stands in for the user directory.
type fakeDirectory struct {
	users []tripcar.User
	err   error
	calls int
}

func (f *fakeDirectory) Users(context.Context) ([]tripcar.User, error) {
	f.calls++
	return f.users, f.err
}

func validKey() string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)) }

const (
	prodURI   = "https://mdm.isha.net/api/settings/google-calendar/callback"
	devURI    = "http://localhost:5173/api/settings/google-calendar/callback"
	oauthJSON = `{"web":{"client_id":"123-abc.apps.googleusercontent.com","client_secret":"GOCSPX-TOPSECRET","redirect_uris":["` + prodURI + `","` + devURI + `"]}}`
	credsPath = "/api/settings/google-calendar/credentials"
	basePath  = "/api/settings/google-calendar"
)

type env struct {
	c    *GoogleCalendarController
	repo *fakeGoogleRepo
	g    *fakeGoogle
	dir  *fakeDirectory
	mux  *http.ServeMux
	now  time.Time
}

func newEnv(t *testing.T, key string) *env {
	t.Helper()
	e := &env{repo: &fakeGoogleRepo{}, g: &fakeGoogle{}, now: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)}
	e.dir = &fakeDirectory{}
	e.c = NewGoogleCalendarController(e.repo, key, middleware.NewAuthHelper(testJWTSecret, nil), e.g, e.dir)
	e.c.now = func() time.Time { return e.now }
	e.mux = http.NewServeMux()
	e.c.RegisterRoutes(e.mux)
	return e
}

func (e *env) uploadCreds(t *testing.T) {
	t.Helper()
	if rec := do(e.mux, "POST", credsPath, tokenFor(t, "admin", "sys_admin"), "application/json", []byte(oauthJSON)); rec.Code != 200 {
		t.Fatalf("upload failed: %d %s", rec.Code, rec.Body)
	}
}

func tokenForUser(t *testing.T, uid, role, systemRole string) string {
	t.Helper()
	access, _, _, err := middleware.GenerateTokens(testJWTSecret, uid, "tester", role, systemRole)
	if err != nil {
		t.Fatal(err)
	}
	return access
}

func tokenFor(t *testing.T, role, systemRole string) string {
	return tokenForUser(t, "user-1", role, systemRole)
}

func do(mux http.Handler, method, path, token, contentType string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func multipartBody(t *testing.T, field, content string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, "client_secret_123.json")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write([]byte(content))
	mw.Close()
	return buf.Bytes(), mw.FormDataContentType()
}

// ---------------- credentials upload ----------------

func TestUpload_EncryptsSecretAtRestAndNeverReturnsIt(t *testing.T) {
	e := newEnv(t, validKey())
	body, ct := multipartBody(t, "file", oauthJSON)

	rec := do(e.mux, "POST", credsPath, tokenFor(t, "admin", "sys_admin"), ct, body)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if e.repo.s.ClientID != "123-abc.apps.googleusercontent.com" {
		t.Errorf("client id: %q", e.repo.s.ClientID)
	}
	if strings.Contains(e.repo.s.ClientSecretEnc, "TOPSECRET") || !strings.HasPrefix(e.repo.s.ClientSecretEnc, "v1:") {
		t.Errorf("secret not encrypted at rest: %q", e.repo.s.ClientSecretEnc)
	}
	if got, err := e.c.clientSecret(&e.repo.s); err != nil || got != "GOCSPX-TOPSECRET" {
		t.Errorf("decrypt: %q, %v", got, err)
	}
	if strings.Contains(rec.Body.String(), "TOPSECRET") || strings.Contains(rec.Body.String(), e.repo.s.ClientSecretEnc) {
		t.Errorf("response leaks the secret: %s", rec.Body)
	}
	var st googleCalendarStatus
	json.Unmarshal(rec.Body.Bytes(), &st)
	if !st.HasClientSecret || st.Connected || st.ClientID == "" || len(st.RedirectURIs) != 2 || st.CallbackPath == "" {
		t.Errorf("unexpected status %+v", st)
	}
}

func TestUpload_AcceptsRawJSONBody(t *testing.T) {
	e := newEnv(t, validKey())
	e.uploadCreds(t)
	if e.repo.s.ClientSecretEnc == "" {
		t.Error("nothing stored")
	}
}

func TestUpload_ReplacingCredentialsDropsAndRevokesOldToken(t *testing.T) {
	e := newEnv(t, validKey())
	e.uploadCreds(t)
	enc, _ := e.c.box.Encrypt("1//OLD-REFRESH", purposeGoogleRefreshToken)
	e.repo.s.RefreshTokenEnc, e.repo.s.ConnectedAccount = enc, "old@mail.isha.org.tw"

	e.uploadCreds(t)
	if e.repo.s.RefreshTokenEnc != "" || e.repo.s.ConnectedAccount != "" {
		t.Error("a token issued to the old client must not survive new credentials")
	}
	if len(e.g.revoked) != 1 || e.g.revoked[0] != "1//OLD-REFRESH" {
		t.Errorf("old token should be revoked at Google, got %v", e.g.revoked)
	}
}

func TestUpload_RejectsBadFilesWithoutStoringAnything(t *testing.T) {
	cases := map[string]string{
		"service account": `{"type":"service_account","private_key":"-----BEGIN PRIVATE KEY-----","client_email":"a@b.iam.gserviceaccount.com"}`,
		"not json":        `hello`,
		"missing secret":  `{"web":{"client_id":"x"}}`,
		"wrong shape":     `{"foo":"bar"}`,
	}
	for name, in := range cases {
		e := newEnv(t, validKey())
		rec := do(e.mux, "POST", credsPath, tokenFor(t, "admin", "sys_admin"), "application/json", []byte(in))
		if rec.Code != 400 {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
		if e.repo.s.ClientID != "" || e.repo.s.ClientSecretEnc != "" {
			t.Errorf("%s: something was stored: %+v", name, e.repo.s)
		}
	}
}

func TestUpload_MultipartWithoutFileFieldIs400(t *testing.T) {
	e := newEnv(t, validKey())
	body, ct := multipartBody(t, "wrong_field", oauthJSON)
	if rec := do(e.mux, "POST", credsPath, tokenFor(t, "admin", "sys_admin"), ct, body); rec.Code != 400 {
		t.Errorf("status %d, want 400", rec.Code)
	}
}

func TestUpload_OversizedBodyIsRejected(t *testing.T) {
	e := newEnv(t, validKey())
	big := `{"web":{"client_id":"x","client_secret":"` + strings.Repeat("a", 100<<10) + `"}}`
	if rec := do(e.mux, "POST", credsPath, tokenFor(t, "admin", "sys_admin"), "application/json", []byte(big)); rec.Code != 400 {
		t.Errorf("status %d, want 400", rec.Code)
	}
	if e.repo.s.ClientSecretEnc != "" {
		t.Error("oversized upload must not be stored")
	}
}

func TestEverythingRefusesWithoutAValidKey(t *testing.T) {
	for name, key := range map[string]string{"unset": "", "malformed": "change-me-in-production"} {
		e := newEnv(t, key)
		admin := tokenFor(t, "admin", "sys_admin")
		for _, c := range []struct{ path, body string }{
			{credsPath, oauthJSON},
			{basePath + "/connect", `{"origin":"https://mdm.isha.net"}`},
		} {
			rec := do(e.mux, "POST", c.path, admin, "application/json", []byte(c.body))
			if rec.Code != 503 || !strings.Contains(rec.Body.String(), "SETTINGS_ENCRYPTION_KEY") {
				t.Errorf("%s %s: status %d body %s", name, c.path, rec.Code, rec.Body)
			}
		}
		if e.repo.s.ClientSecretEnc != "" || e.repo.s.ClientID != "" {
			t.Errorf("%s: nothing may be stored without a valid key: %+v", name, e.repo.s)
		}
	}
}

func TestEndpoints_RequireSysAdmin(t *testing.T) {
	e := newEnv(t, validKey())
	for _, c := range []struct{ method, path string }{
		{"GET", basePath}, {"DELETE", basePath}, {"POST", credsPath},
		{"POST", basePath + "/connect"}, {"POST", basePath + "/test"}, {"POST", basePath + "/disconnect"},
	} {
		for name, tok := range map[string]string{"anonymous": "", "ordinary user": tokenFor(t, "user", "")} {
			rec := do(e.mux, c.method, c.path, tok, "application/json", []byte(oauthJSON))
			if rec.Code != 401 {
				t.Errorf("%s %s as %s: status %d, want 401", c.method, c.path, name, rec.Code)
			}
		}
	}
	if e.repo.s.ClientSecretEnc != "" {
		t.Error("an unauthorised upload was stored")
	}
}

func TestStatusAndDelete(t *testing.T) {
	e := newEnv(t, validKey())
	admin := tokenFor(t, "admin", "sys_admin")

	rec := do(e.mux, "GET", basePath, admin, "", nil)
	var st googleCalendarStatus
	json.Unmarshal(rec.Body.Bytes(), &st)
	if rec.Code != 200 || !st.EncryptionReady || st.HasClientSecret || st.Connected || st.RedirectURIs == nil || st.SelectedCalendarIDs == nil {
		t.Errorf("fresh state: %d %+v (redirect_uris must be [] not null)", rec.Code, st)
	}

	e.uploadCreds(t)
	enc, _ := e.c.box.Encrypt("1//REFRESH", purposeGoogleRefreshToken)
	e.repo.s.RefreshTokenEnc, e.repo.s.ConnectedAccount = enc, "me@mail.isha.org.tw"
	rec = do(e.mux, "GET", basePath, admin, "", nil)
	json.Unmarshal(rec.Body.Bytes(), &st)
	if !st.HasClientSecret || !st.Connected || st.ConnectedAccount != "me@mail.isha.org.tw" ||
		strings.Contains(rec.Body.String(), "REFRESH") || strings.Contains(rec.Body.String(), enc) {
		t.Errorf("after connecting: %+v body=%s", st, rec.Body)
	}

	if rec := do(e.mux, "DELETE", basePath, admin, "", nil); rec.Code != 200 {
		t.Fatalf("delete status %d", rec.Code)
	}
	if e.repo.s.ClientID != "" || e.repo.s.ClientSecretEnc != "" || e.repo.s.RefreshTokenEnc != "" || len(e.repo.s.RedirectURIs) != 0 {
		t.Errorf("delete should wipe everything: %+v", e.repo.s)
	}
	if len(e.g.revoked) != 1 || e.g.revoked[0] != "1//REFRESH" {
		t.Errorf("delete should revoke the token at Google, got %v", e.g.revoked)
	}
}

func TestDeleteStillWorksWhenRevokeFails(t *testing.T) {
	e := newEnv(t, validKey())
	e.uploadCreds(t)
	enc, _ := e.c.box.Encrypt("1//REFRESH", purposeGoogleRefreshToken)
	e.repo.s.RefreshTokenEnc = enc
	e.g.revokeErr = errors.New("network down")
	if rec := do(e.mux, "DELETE", basePath, tokenFor(t, "admin", "sys_admin"), "", nil); rec.Code != 200 {
		t.Errorf("status %d", rec.Code)
	}
	if e.repo.s.ClientSecretEnc != "" {
		t.Error("local removal must not depend on Google being reachable")
	}
}

func TestStatusReportsWhyEncryptionIsOff(t *testing.T) {
	e := newEnv(t, "")
	rec := do(e.mux, "GET", basePath, tokenFor(t, "admin", "sys_admin"), "", nil)
	var st googleCalendarStatus
	json.Unmarshal(rec.Body.Bytes(), &st)
	if st.EncryptionReady || st.EncryptionError == "" {
		t.Errorf("should say encryption isn't ready and why: %+v", st)
	}
}

func TestDecryptWithDifferentKeyFails(t *testing.T) {
	e := newEnv(t, validKey())
	e.uploadCreds(t)
	other := NewGoogleCalendarController(e.repo, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), middleware.NewAuthHelper(testJWTSecret, nil), e.g, nil)
	if _, err := other.clientSecret(&e.repo.s); err == nil {
		t.Error("decrypting with a different key must fail")
	}
}

// ---------------- connect ----------------

func connect(t *testing.T, e *env, origin string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	rec := do(e.mux, "POST", basePath+"/connect", tokenFor(t, "admin", "sys_admin"), "application/json", []byte(`{"origin":"`+origin+`"}`))
	var out struct {
		URL         string `json:"url"`
		RedirectURI string `json:"redirect_uri"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out.URL
}

func TestConnect_ReturnsAuthURLForARegisteredOrigin(t *testing.T) {
	e := newEnv(t, validKey())
	e.uploadCreds(t)
	for origin, wantURI := range map[string]string{"https://mdm.isha.net": prodURI, "http://localhost:5173": devURI} {
		rec, authURL := connect(t, e, origin)
		if rec.Code != 200 {
			t.Fatalf("%s: status %d %s", origin, rec.Code, rec.Body)
		}
		u, _ := url.Parse(authURL)
		if u.Query().Get("redirect_uri") != wantURI || u.Query().Get("client_id") != "123-abc.apps.googleusercontent.com" || u.Query().Get("state") == "" {
			t.Errorf("%s: auth url %s", origin, authURL)
		}
		if strings.Contains(authURL, "TOPSECRET") {
			t.Error("the auth URL must never carry the client secret")
		}
	}
}

func TestConnect_Rejections(t *testing.T) {
	e := newEnv(t, validKey())
	if rec, _ := connect(t, e, "https://mdm.isha.net"); rec.Code != 400 || !strings.Contains(rec.Body.String(), "上傳") {
		t.Errorf("before upload: %d %s", rec.Code, rec.Body)
	}
	e.uploadCreds(t)
	for _, origin := range []string{"https://evil.example.com", "https://mdm.isha.net.evil.com", "http://mdm.isha.net", "not a url", ""} {
		if rec, _ := connect(t, e, origin); rec.Code != 400 {
			t.Errorf("origin %q: status %d, want 400", origin, rec.Code)
		}
	}
}

// ---------------- callback ----------------

// startFlow runs connect as `uid` and returns the state Google would hand back.
func startFlow(t *testing.T, e *env, uid, origin string) string {
	t.Helper()
	rec := do(e.mux, "POST", basePath+"/connect", tokenForUser(t, uid, "admin", "sys_admin"), "application/json", []byte(`{"origin":"`+origin+`"}`))
	var out struct{ URL string }
	json.Unmarshal(rec.Body.Bytes(), &out)
	u, _ := url.Parse(out.URL)
	return u.Query().Get("state")
}

func callback(e *env, token string, query url.Values) *httptest.ResponseRecorder {
	return do(e.mux, "GET", domain.GoogleCalendarCallbackPath+"?"+query.Encode(), token, "", nil)
}

func resultOf(t *testing.T, rec *httptest.ResponseRecorder) (outcome, msg string) {
	t.Helper()
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("callback should redirect, got %d %s", rec.Code, rec.Body)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || loc.Path != "/admin/settings" {
		t.Fatalf("redirect to %q", rec.Header().Get("Location"))
	}
	return loc.Query().Get("google_calendar"), loc.Query().Get("msg")
}

func TestCallback_CompletesTheFlowAndStoresAnEncryptedToken(t *testing.T) {
	e := newEnv(t, validKey())
	e.uploadCreds(t)
	e.g.exchangeToken = &google.Token{AccessToken: "ya29.A", RefreshToken: "1//THE-REFRESH-TOKEN", ExpiresIn: 3599}
	e.g.calendars = []google.Calendar{{ID: "trips@group.calendar.google.com", Summary: "出差"}, {ID: "me@mail.isha.org.tw", Primary: true}}
	state := startFlow(t, e, "admin-1", "https://mdm.isha.net")

	rec := callback(e, tokenForUser(t, "admin-1", "admin", "sys_admin"), url.Values{"code": {"auth-code"}, "state": {state}})
	if outcome, msg := resultOf(t, rec); outcome != "connected" {
		t.Fatalf("outcome %q msg %q", outcome, msg)
	}
	// Exchanged with the decrypted secret, the code, and the very URI the flow began with.
	if e.g.gotCode != "auth-code" || e.g.gotClientSecret != "GOCSPX-TOPSECRET" || e.g.gotRedirectURI != prodURI || e.g.gotClientID != "123-abc.apps.googleusercontent.com" {
		t.Errorf("exchange args: code=%q secret=%q uri=%q id=%q", e.g.gotCode, e.g.gotClientSecret, e.g.gotRedirectURI, e.g.gotClientID)
	}
	if e.g.listedWith != "ya29.A" {
		t.Errorf("should verify calendars are readable with the new access token, got %q", e.g.listedWith)
	}
	// Stored encrypted, bound to the refresh-token purpose, with the primary calendar's address as the account.
	if strings.Contains(e.repo.s.RefreshTokenEnc, "THE-REFRESH-TOKEN") || !strings.HasPrefix(e.repo.s.RefreshTokenEnc, "v1:") {
		t.Errorf("refresh token not encrypted: %q", e.repo.s.RefreshTokenEnc)
	}
	if got, err := e.c.refreshToken(&e.repo.s); err != nil || got != "1//THE-REFRESH-TOKEN" {
		t.Errorf("stored token decrypts to %q, %v", got, err)
	}
	if _, err := e.c.box.Decrypt(e.repo.s.RefreshTokenEnc, purposeGoogleClientSecret); err == nil {
		t.Error("the refresh token ciphertext must not be usable as a client secret")
	}
	if e.repo.s.ConnectedAccount != "me@mail.isha.org.tw" {
		t.Errorf("connected account %q", e.repo.s.ConnectedAccount)
	}
}

func TestCallback_StateMustBeGenuineFreshAndForThisUser(t *testing.T) {
	e := newEnv(t, validKey())
	e.uploadCreds(t)
	e.g.exchangeToken = &google.Token{RefreshToken: "1//R"}
	good := startFlow(t, e, "admin-1", "https://mdm.isha.net")

	otherKeyEnv := newEnv(t, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)))
	forged, _ := makeOAuthState(otherKeyEnv.c.box, "admin-1", prodURI, e.now)

	cases := []struct {
		name  string
		token string
		state string
		later time.Duration
	}{
		{"missing state", tokenForUser(t, "admin-1", "admin", "sys_admin"), "", 0},
		{"garbage state", tokenForUser(t, "admin-1", "admin", "sys_admin"), "v1:AAAA", 0},
		{"state sealed with another key", tokenForUser(t, "admin-1", "admin", "sys_admin"), forged, 0},
		{"state issued to a different admin", tokenForUser(t, "admin-2", "admin", "sys_admin"), good, 0},
		{"state past its expiry", tokenForUser(t, "admin-1", "admin", "sys_admin"), good, oauthStateTTL + time.Second},
	}
	for _, c := range cases {
		e.now = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC).Add(c.later)
		e.g.gotCode = ""
		rec := callback(e, c.token, url.Values{"code": {"auth-code"}, "state": {c.state}})
		if outcome, _ := resultOf(t, rec); outcome != "error" {
			t.Errorf("%s: should be rejected", c.name)
		}
		if e.g.gotCode != "" {
			t.Errorf("%s: must not even attempt the code exchange", c.name)
		}
		if e.repo.s.RefreshTokenEnc != "" {
			t.Errorf("%s: nothing may be stored", c.name)
		}
	}
}

func TestCallback_RequiresALoggedInSysAdmin(t *testing.T) {
	e := newEnv(t, validKey())
	e.uploadCreds(t)
	state := startFlow(t, e, "admin-1", "https://mdm.isha.net")
	for name, tok := range map[string]string{"anonymous": "", "ordinary user": tokenForUser(t, "admin-1", "user", "")} {
		rec := callback(e, tok, url.Values{"code": {"c"}, "state": {state}})
		if outcome, _ := resultOf(t, rec); outcome != "error" {
			t.Errorf("%s: should be rejected", name)
		}
	}
	if e.g.gotCode != "" || e.repo.s.RefreshTokenEnc != "" {
		t.Error("an unauthenticated callback must do nothing")
	}
}

func TestCallback_UserDeclinedAndGoogleErrors(t *testing.T) {
	e := newEnv(t, validKey())
	e.uploadCreds(t)
	admin := tokenForUser(t, "admin-1", "admin", "sys_admin")

	if _, msg := resultOf(t, callback(e, admin, url.Values{"error": {"access_denied"}})); !strings.Contains(msg, "取消") {
		t.Errorf("declined: %q", msg)
	}

	state := startFlow(t, e, "admin-1", "https://mdm.isha.net")
	for name, c := range map[string]struct {
		err  error
		want string
	}{
		"redirect_uri_mismatch": {&google.APIError{Status: 400, Code: "redirect_uri_mismatch"}, "redirect_uri_mismatch"},
		"invalid_client":        {&google.APIError{Status: 401, Code: "invalid_client"}, "重新上傳"},
		"invalid_grant":         {&google.APIError{Status: 400, Code: "invalid_grant"}, "重新連結"},
	} {
		e.g.exchangeErr = c.err
		outcome, msg := resultOf(t, callback(e, admin, url.Values{"code": {"x"}, "state": {state}}))
		if outcome != "error" || !strings.Contains(msg, c.want) {
			t.Errorf("%s: outcome %q msg %q", name, outcome, msg)
		}
		if strings.Contains(msg, "TOPSECRET") {
			t.Errorf("%s: message leaks the secret", name)
		}
	}
	if e.repo.s.RefreshTokenEnc != "" {
		t.Error("failed exchanges must not store anything")
	}
}

func TestCallback_NoRefreshTokenFromGoogleIsAnExplainedFailure(t *testing.T) {
	e := newEnv(t, validKey())
	e.uploadCreds(t)
	e.g.exchangeToken = &google.Token{AccessToken: "ya29.A"} // no refresh token
	state := startFlow(t, e, "admin-1", "https://mdm.isha.net")
	outcome, msg := resultOf(t, callback(e, tokenForUser(t, "admin-1", "admin", "sys_admin"), url.Values{"code": {"x"}, "state": {state}}))
	if outcome != "error" || !strings.Contains(msg, "permissions") || e.repo.s.RefreshTokenEnc != "" {
		t.Errorf("outcome %q msg %q stored %q", outcome, msg, e.repo.s.RefreshTokenEnc)
	}
}

func TestCallback_NotConnectedIfCalendarsCantBeRead(t *testing.T) {
	// e.g. the Calendar API isn't enabled in the Google project: say so now,
	// rather than reporting "connected" and failing later in a background job.
	e := newEnv(t, validKey())
	e.uploadCreds(t)
	e.g.exchangeToken = &google.Token{AccessToken: "ya29.A", RefreshToken: "1//R"}
	e.g.listErr = &google.APIError{Status: 403, Code: "PERMISSION_DENIED", Description: "Google Calendar API has not been used in project 1 before or it is disabled."}
	state := startFlow(t, e, "admin-1", "https://mdm.isha.net")
	outcome, msg := resultOf(t, callback(e, tokenForUser(t, "admin-1", "admin", "sys_admin"), url.Values{"code": {"x"}, "state": {state}}))
	if outcome != "error" || !strings.Contains(msg, "Google Calendar API") || e.repo.s.RefreshTokenEnc != "" {
		t.Errorf("outcome %q msg %q stored %q", outcome, msg, e.repo.s.RefreshTokenEnc)
	}
}

func TestCallback_ReplacingAConnectionRevokesTheOldToken(t *testing.T) {
	e := newEnv(t, validKey())
	e.uploadCreds(t)
	old, _ := e.c.box.Encrypt("1//OLD", purposeGoogleRefreshToken)
	e.repo.s.RefreshTokenEnc = old
	e.g.exchangeToken = &google.Token{AccessToken: "ya29.A", RefreshToken: "1//NEW"}
	e.g.calendars = []google.Calendar{{ID: "me@x", Primary: true}}
	state := startFlow(t, e, "admin-1", "https://mdm.isha.net")
	if outcome, _ := resultOf(t, callback(e, tokenForUser(t, "admin-1", "admin", "sys_admin"), url.Values{"code": {"x"}, "state": {state}})); outcome != "connected" {
		t.Fatal("expected success")
	}
	if len(e.g.revoked) != 1 || e.g.revoked[0] != "1//OLD" {
		t.Errorf("old token should be revoked, got %v", e.g.revoked)
	}
	if got, _ := e.c.refreshToken(&e.repo.s); got != "1//NEW" {
		t.Errorf("stored token %q", got)
	}
}

// ---------------- test connection / disconnect ----------------

func connectedEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t, validKey())
	e.uploadCreds(t)
	enc, _ := e.c.box.Encrypt("1//REFRESH", purposeGoogleRefreshToken)
	e.repo.s.RefreshTokenEnc, e.repo.s.ConnectedAccount = enc, "me@mail.isha.org.tw"
	return e
}

func TestTestConnection_ListsCalendarsUsingTheStoredToken(t *testing.T) {
	e := connectedEnv(t)
	e.g.calendars = []google.Calendar{{ID: "me@mail.isha.org.tw", Summary: "me", AccessRole: "owner", Primary: true}, {ID: "trips@g", Summary: "出差", AccessRole: "reader"}}
	rec := do(e.mux, "POST", basePath+"/test", tokenFor(t, "admin", "sys_admin"), "", nil)
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	if e.g.gotRefresh != "1//REFRESH" || e.g.gotClientSecret != "GOCSPX-TOPSECRET" || e.g.listedWith != "ya29.ACCESS" {
		t.Errorf("flow: refresh=%q secret=%q listedWith=%q", e.g.gotRefresh, e.g.gotClientSecret, e.g.listedWith)
	}
	var out struct {
		Account   string            `json:"account"`
		Calendars []google.Calendar `json:"calendars"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Account != "me@mail.isha.org.tw" || len(out.Calendars) != 2 || out.Calendars[1].Summary != "出差" {
		t.Errorf("response %+v", out)
	}
	for _, leak := range []string{"TOPSECRET", "1//REFRESH", "ya29"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("response leaks %q", leak)
		}
	}
}

func TestTestConnection_Failures(t *testing.T) {
	admin := tokenFor(t, "admin", "sys_admin")

	// not connected yet
	e := newEnv(t, validKey())
	e.uploadCreds(t)
	if rec := do(e.mux, "POST", basePath+"/test", admin, "", nil); rec.Code != 400 {
		t.Errorf("not connected: status %d", rec.Code)
	}

	// token revoked at Google: 409 and a message to reconnect
	e = connectedEnv(t)
	e.g.refreshErr = &google.APIError{Status: 400, Code: "invalid_grant", Description: "Token has been expired or revoked."}
	if rec := do(e.mux, "POST", basePath+"/test", admin, "", nil); rec.Code != 409 || !strings.Contains(rec.Body.String(), "重新連結") {
		t.Errorf("invalid_grant: %d %s", rec.Code, rec.Body)
	}

	// Google unreachable: a gateway error, not a crash
	e = connectedEnv(t)
	e.g.listErr = errors.New("無法連線到 Google")
	if rec := do(e.mux, "POST", basePath+"/test", admin, "", nil); rec.Code != 502 {
		t.Errorf("list failure: status %d", rec.Code)
	}

	// no calendars at all comes back as [] rather than null
	e = connectedEnv(t)
	rec := do(e.mux, "POST", basePath+"/test", admin, "", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"calendars":[]`) {
		t.Errorf("empty list: %d %s", rec.Code, rec.Body)
	}
}

func TestDisconnect_KeepsCredentialsDropsTokenAndRevokes(t *testing.T) {
	e := connectedEnv(t)
	rec := do(e.mux, "POST", basePath+"/disconnect", tokenFor(t, "admin", "sys_admin"), "", nil)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if e.repo.s.RefreshTokenEnc != "" || e.repo.s.ConnectedAccount != "" {
		t.Errorf("token should be gone: %+v", e.repo.s)
	}
	if e.repo.s.ClientID == "" || e.repo.s.ClientSecretEnc == "" || len(e.repo.s.RedirectURIs) == 0 {
		t.Errorf("credentials must be kept so the admin can reconnect: %+v", e.repo.s)
	}
	if len(e.g.revoked) != 1 || e.g.revoked[0] != "1//REFRESH" {
		t.Errorf("revoked: %v", e.g.revoked)
	}
	var st googleCalendarStatus
	json.Unmarshal(rec.Body.Bytes(), &st)
	if st.Connected || !st.HasClientSecret {
		t.Errorf("status after disconnect: %+v", st)
	}
}

// ---------------- choosing calendars ----------------

func TestSelectCalendars(t *testing.T) {
	e := connectedEnv(t)
	admin := tokenFor(t, "admin", "sys_admin")
	put := func(body string) *httptest.ResponseRecorder {
		return do(e.mux, "PUT", basePath+"/calendars", admin, "application/json", []byte(body))
	}

	rec := put(`{"calendar_ids":[" bip@group ","ida@group","bip@group","","  "]}`)
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	if got := strings.Join(e.repo.s.SelectedCalendarIDs, ","); got != "bip@group,ida@group" {
		t.Errorf("trimmed, de-duplicated, order kept: %q", got)
	}
	var st googleCalendarStatus
	json.Unmarshal(rec.Body.Bytes(), &st)
	if len(st.SelectedCalendarIDs) != 2 {
		t.Errorf("status should echo the selection: %+v", st)
	}

	if rec := put(`{"calendar_ids":[]}`); rec.Code != 200 || len(e.repo.s.SelectedCalendarIDs) != 0 {
		t.Errorf("clearing the selection: %d %v", rec.Code, e.repo.s.SelectedCalendarIDs)
	}

	tooMany := make([]string, 21)
	for i := range tooMany {
		tooMany[i] = "cal-" + string(rune('a'+i))
	}
	b, _ := json.Marshal(map[string]any{"calendar_ids": tooMany})
	if rec := put(string(b)); rec.Code != 400 {
		t.Errorf("21 calendars: status %d", rec.Code)
	}
	if rec := put(`{"calendar_ids":["` + strings.Repeat("x", 301) + `"]}`); rec.Code != 400 {
		t.Errorf("overlong id: status %d", rec.Code)
	}
	if rec := put(`nope`); rec.Code != 400 {
		t.Errorf("bad body: status %d", rec.Code)
	}
}

func TestSelectCalendars_NeedsAConnectionAndAdmin(t *testing.T) {
	e := newEnv(t, validKey())
	e.uploadCreds(t) // credentials but no authorised account
	if rec := do(e.mux, "PUT", basePath+"/calendars", tokenFor(t, "admin", "sys_admin"), "application/json", []byte(`{"calendar_ids":["a"]}`)); rec.Code != 400 {
		t.Errorf("not connected: status %d", rec.Code)
	}
	e = connectedEnv(t)
	if rec := do(e.mux, "PUT", basePath+"/calendars", tokenFor(t, "user", ""), "application/json", []byte(`{"calendar_ids":["a"]}`)); rec.Code != 401 {
		t.Errorf("ordinary user: status %d", rec.Code)
	}
	if len(e.repo.s.SelectedCalendarIDs) != 0 {
		t.Error("an unauthorised request changed the selection")
	}
	if rec := do(e.mux, "GET", basePath+"/calendars", tokenFor(t, "admin", "sys_admin"), "", nil); rec.Code != 405 {
		t.Errorf("GET: status %d, want 405", rec.Code)
	}
}

// ---------------- previewing events ----------------

type previewResp struct {
	Days      int `json:"days"`
	Calendars []struct {
		ID, Summary string
		Count       int
		Error       string
	} `json:"calendars"`
	Events []struct {
		ID              string `json:"id"`
		Summary         string `json:"summary"`
		Start           string `json:"start"`
		CalendarSummary string `json:"calendar_summary"`
		Trip            struct {
			Location string   `json:"location"`
			Names    []string `json:"names"`
			Emails   []string `json:"emails"`
		} `json:"trip"`
		LocationUsed   string `json:"location_used"`
		LocationSource string `json:"location_source"`
		TargetLocation bool   `json:"target_location"`
	} `json:"events"`
}

func previewEnv(t *testing.T) *env {
	t.Helper()
	e := connectedEnv(t)
	e.repo.s.SelectedCalendarIDs = []string{"bip@group", "ida@group"}
	e.g.calendars = []google.Calendar{{ID: "bip@group", Summary: "ISHA高雄-園管局BIP計畫"}, {ID: "ida@group", Summary: "ISHA高雄-IDA P70計畫"}, {ID: "other@group", Summary: "not selected"}}
	e.g.events = map[string][]google.Event{
		"bip@group": {
			// the sample from the business: Miaoli/Taichung is not a target
			{ID: "e-miaoli", Summary: "苗栗出差", Start: "2026-10-28T08:00:00+08:00", Description: "地點： 苗栗、台中\n時間：08:00-17:00\n交通：高鐵＋租車\n協會人員：張啓瑞 、劉名政\n信箱：leonchang@mail.isha.org.tw、foylaou0326@mail.isha.org.tw"},
			// Google's web editor saves HTML
			{ID: "e-khh", Summary: "高雄廠區訪視", Start: "2026-10-22T08:00:00+08:00", Description: "<b>地點：</b>高雄、屏東<br>協會人員：吳宜蓁<br>信箱：idsl5397@mail.isha.org.tw"},
		},
		"ida@group": {
			// no 地點 line, but the event's own location field names a target county
			{ID: "e-loc", Summary: "台南會議", Start: "2026-10-21", AllDay: true, Location: "台南市中西區", Description: "只是備註，沒有標籤"},
			{ID: "e-none", Summary: "內部會議", Start: "2026-10-25T10:00:00+08:00"},
			// factory-visit style: destination only in the title's brackets, attendee line without emails
			{ID: "e-visit", Summary: "76工危品進廠(高屏分局)", Start: "2026-10-26", AllDay: true, Description: "進廠人員:羅羿昇\n10:30-12:00 公司A"},
		},
	}
	return e
}

func TestPreview_ParsesEventsAndFlagsTargets(t *testing.T) {
	e := previewEnv(t)
	rec := do(e.mux, "GET", basePath+"/events", tokenFor(t, "admin", "sys_admin"), "", nil)
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	var out previewResp
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Days != 60 || len(out.Calendars) != 2 || len(out.Events) != 5 {
		t.Fatalf("days=%d calendars=%d events=%d", out.Days, len(out.Calendars), len(out.Events))
	}
	if out.Calendars[0].Summary != "ISHA高雄-園管局BIP計畫" || out.Calendars[0].Count != 2 {
		t.Errorf("calendar summary: %+v", out.Calendars[0])
	}

	// Ordered by start across calendars: Oct 21 (all-day), 22, 25, 28.
	order := []string{}
	byID := map[string]int{}
	for i, ev := range out.Events {
		order = append(order, ev.ID)
		byID[ev.ID] = i
	}
	if strings.Join(order, ",") != "e-loc,e-khh,e-none,e-visit,e-miaoli" {
		t.Errorf("order: %v", order)
	}

	miaoli := out.Events[byID["e-miaoli"]]
	if miaoli.TargetLocation || miaoli.LocationSource != "description" || miaoli.Trip.Location != "苗栗、台中" ||
		strings.Join(miaoli.Trip.Names, "|") != "張啓瑞|劉名政" || len(miaoli.Trip.Emails) != 2 || miaoli.CalendarSummary != "ISHA高雄-園管局BIP計畫" {
		t.Errorf("the sample trip should parse but NOT be a target: %+v", miaoli)
	}
	khh := out.Events[byID["e-khh"]]
	if !khh.TargetLocation || khh.Trip.Location != "高雄、屏東" || len(khh.Trip.Emails) != 1 {
		t.Errorf("HTML description / target county: %+v", khh)
	}
	loc := out.Events[byID["e-loc"]]
	if !loc.TargetLocation || loc.LocationSource != "event" || loc.LocationUsed != "台南市中西區" || loc.Trip.Names == nil {
		t.Errorf("fallback to the event's location field: %+v", loc)
	}
	visit := out.Events[byID["e-visit"]]
	if !visit.TargetLocation || visit.LocationSource != "title" || visit.LocationUsed != "高屏分局" || strings.Join(visit.Trip.Names, "|") != "羅羿昇" {
		t.Errorf("factory-visit event: %+v", visit)
	}
	none := out.Events[byID["e-none"]]
	if none.TargetLocation || none.LocationSource != "" || none.Trip.Emails == nil || none.Trip.Names == nil {
		t.Errorf("event with nothing to parse (lists must be [] not null): %+v", none)
	}
}

func TestPreview_OnlyReadsSelectedCalendarsInTheRightWindow(t *testing.T) {
	e := previewEnv(t)
	rec := do(e.mux, "GET", basePath+"/events?days=30", tokenFor(t, "admin", "sys_admin"), "", nil)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if strings.Join(e.g.eventCalls, ",") != "bip@group,ida@group" {
		t.Errorf("must read exactly the selected calendars, got %v", e.g.eventCalls)
	}
	if want := e.now.Add(-24 * time.Hour); !e.g.eventsFrom.Equal(want) {
		t.Errorf("window should start a day back so an ongoing trip shows: from %v want %v", e.g.eventsFrom, want)
	}
	if want := e.now.AddDate(0, 0, 30); !e.g.eventsTo.Equal(want) {
		t.Errorf("window end: %v want %v", e.g.eventsTo, want)
	}
	if e.g.listedWith != "ya29.ACCESS" {
		t.Errorf("used token %q", e.g.listedWith)
	}
	for _, leak := range []string{"TOPSECRET", "1//REFRESH", "ya29"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("response leaks %q", leak)
		}
	}
}

func TestPreview_OneCalendarFailingDoesNotHideTheOthers(t *testing.T) {
	e := previewEnv(t)
	e.g.eventsErr = map[string]error{"bip@group": &google.APIError{Status: 404, Code: "NOT_FOUND", Description: "Not Found"}}
	e.g.events["bip@group"] = nil
	rec := do(e.mux, "GET", basePath+"/events", tokenFor(t, "admin", "sys_admin"), "", nil)
	var out previewResp
	json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || out.Calendars[0].Error == "" || out.Calendars[1].Error != "" || len(out.Events) != 3 {
		t.Errorf("status %d calendars %+v events %d", rec.Code, out.Calendars, len(out.Events))
	}
}

func TestPreview_LongDescriptionsAreTruncated(t *testing.T) {
	e := previewEnv(t)
	e.g.events = map[string][]google.Event{"bip@group": {{ID: "big", Start: "2026-10-22", AllDay: true, Description: "地點：高雄\n" + strings.Repeat("字", 9000)}}}
	rec := do(e.mux, "GET", basePath+"/events", tokenFor(t, "admin", "sys_admin"), "", nil)
	var raw struct {
		Events []struct {
			Description string `json:"description"`
			Truncated   bool   `json:"description_truncated"`
			Target      bool   `json:"target_location"`
		} `json:"events"`
	}
	json.Unmarshal(rec.Body.Bytes(), &raw)
	if len(raw.Events) != 1 || !raw.Events[0].Truncated || len([]rune(raw.Events[0].Description)) != 4000 || !raw.Events[0].Target {
		t.Errorf("should truncate only the displayed text, still parse the rest: %+v", raw.Events)
	}
}

func TestPreview_Rejections(t *testing.T) {
	admin := tokenFor(t, "admin", "sys_admin")

	e := connectedEnv(t) // connected, but no calendar chosen yet
	if rec := do(e.mux, "GET", basePath+"/events", admin, "", nil); rec.Code != 400 || !strings.Contains(rec.Body.String(), "選擇") {
		t.Errorf("no selection: %d %s", rec.Code, rec.Body)
	}
	e = previewEnv(t)
	for _, q := range []string{"days=0", "days=-3", "days=367", "days=abc"} {
		if rec := do(e.mux, "GET", basePath+"/events?"+q, admin, "", nil); rec.Code != 400 {
			t.Errorf("%s: status %d, want 400", q, rec.Code)
		}
	}
	if len(e.g.eventCalls) != 0 {
		t.Error("an invalid request must not reach Google")
	}
	if rec := do(e.mux, "GET", basePath+"/events", tokenFor(t, "user", ""), "", nil); rec.Code != 401 {
		t.Errorf("ordinary user: status %d", rec.Code)
	}
	e.g.refreshErr = &google.APIError{Status: 400, Code: "invalid_grant"}
	if rec := do(e.mux, "GET", basePath+"/events", admin, "", nil); rec.Code != 409 || !strings.Contains(rec.Body.String(), "重新連結") {
		t.Errorf("revoked token: %d %s", rec.Code, rec.Body)
	}
}

func TestCleanCalendarIDs(t *testing.T) {
	got, err := cleanCalendarIDs([]string{"a", " b ", "a", "", "c"})
	if err != nil || strings.Join(got, ",") != "a,b,c" {
		t.Errorf("got %v, %v", got, err)
	}
	if got, _ := cleanCalendarIDs(nil); got == nil || len(got) != 0 {
		t.Errorf("nil input should give an empty, non-nil slice, got %#v", got)
	}
}

// ---------------- preview: who would be notified ----------------

func bp(b bool) *bool { return &b }

func directoryUsers() []tripcar.User {
	return []tripcar.User{
		{SamAccountName: "yishengluo1006", DisplayName: "26安環處-高雄-羅羿昇", Mail: "yishengluo1006@mail.isha.org.tw", HasDriverLicense: bp(true), HasOwnCar: bp(false)},
		{SamAccountName: "zoey.xia", DisplayName: "26安環處-高雄-夏宗瑜", Mail: "zoey.xia@mail.isha.org.tw", HasDriverLicense: bp(true), HasOwnCar: bp(true)},
		{SamAccountName: "s01023", DisplayName: "26安環處-高雄-李念芷", Mail: "s01023@mail.isha.org.tw"},
		{SamAccountName: "idsl5397", DisplayName: "26安環處-高雄-吳宜蓁", Mail: "idsl5397@mail.isha.org.tw", HasDriverLicense: bp(false), HasOwnCar: bp(false)},
		{SamAccountName: "ids", DisplayName: "吳宜蓁"},
	}
}

type candJSON struct {
	Name, Email, Account, Decision string
	Priority                       int
	HasOwnCar                      *bool `json:"has_own_car"`
}

type previewWithCandidates struct {
	Directory struct {
		Configured bool   `json:"configured"`
		Users      int    `json:"users"`
		Error      string `json:"error"`
	} `json:"directory"`
	Events []struct {
		ID         string     `json:"id"`
		Target     bool       `json:"target_location"`
		NoticeDue  bool       `json:"notice_due"`
		NoticeFrom string     `json:"notice_from"`
		DaysUntil  int        `json:"days_until"`
		Candidates []candJSON `json:"candidates"`
	} `json:"events"`
}

func previewWithDirectory(t *testing.T, e *env) (*httptest.ResponseRecorder, previewWithCandidates) {
	t.Helper()
	rec := do(e.mux, "GET", basePath+"/events", tokenFor(t, "admin", "sys_admin"), "", nil)
	var out previewWithCandidates
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err, rec.Body.String())
	}
	return rec, out
}

func (p previewWithCandidates) event(id string) (e struct {
	ID         string     `json:"id"`
	Target     bool       `json:"target_location"`
	NoticeDue  bool       `json:"notice_due"`
	NoticeFrom string     `json:"notice_from"`
	DaysUntil  int        `json:"days_until"`
	Candidates []candJSON `json:"candidates"`
}) {
	for _, ev := range p.Events {
		if ev.ID == id {
			return ev
		}
	}
	return
}

func TestPreview_ShowsWhoWouldBeNotifiedAndInWhatOrder(t *testing.T) {
	e := previewEnv(t)
	e.dir.users = directoryUsers()
	// two people on one trip, listed by name only (進廠人員 style)
	e.g.events["ida@group"] = append(e.g.events["ida@group"], google.Event{
		ID: "e-two", Summary: "76工危品進廠(臺南分局)", Start: "2026-10-27", AllDay: true,
		Description: "進廠人員:夏宗瑜(管考)\n進廠人員:羅羿昇\n進廠人員:李念芷\n進廠人員:路人甲",
	})
	rec, out := previewWithDirectory(t, e)
	if rec.Code != 200 || !out.Directory.Configured || out.Directory.Users != 5 || out.Directory.Error != "" {
		t.Fatalf("%d directory %+v", rec.Code, out.Directory)
	}

	two := out.event("e-two")
	if !two.Target || len(two.Candidates) != 4 {
		t.Fatalf("e-two: %+v", two)
	}
	// Approval order: no car first (羅羿昇), unknown next (李念芷), then owner (夏宗瑜), then not found.
	var order []string
	for _, c := range two.Candidates {
		order = append(order, c.Name+":"+c.Decision)
	}
	if strings.Join(order, ",") != "羅羿昇:notify,李念芷:missing_data,夏宗瑜:notify,路人甲:unmatched" {
		t.Errorf("order/decisions: %v", order)
	}
	if two.Candidates[0].Account != "yishengluo1006" || two.Candidates[0].HasOwnCar == nil || *two.Candidates[0].HasOwnCar {
		t.Errorf("first candidate: %+v", two.Candidates[0])
	}

	// A trip with both a name and a mail for the same person doesn't list them twice;
	// 吳宜蓁 explicitly has no license.
	khh := out.event("e-khh")
	if len(khh.Candidates) != 1 || khh.Candidates[0].Decision != "no_license" || khh.Candidates[0].Account != "idsl5397" {
		t.Errorf("e-khh: %+v", khh.Candidates)
	}

	// Not a target destination: nothing is evaluated at all.
	if m := out.event("e-miaoli"); m.Target || len(m.Candidates) != 0 || m.NoticeDue {
		t.Errorf("a non-target trip must have no candidates and no notice: %+v", m)
	}
}

func TestPreview_NoticeWindow(t *testing.T) {
	// now = 2026-10-20 17:00 Taipei
	e := previewEnv(t)
	e.dir.users = directoryUsers()
	e.g.events["ida@group"] = []google.Event{
		{ID: "far", Summary: "遠(臺南分局)", Start: "2026-10-28", AllDay: true, Description: "進廠人員:羅羿昇"},     // 8 days: window not open
		{ID: "edge", Summary: "邊(臺南分局)", Start: "2026-10-27", AllDay: true, Description: "進廠人員:羅羿昇"},    // exactly 7 days: opens today
		{ID: "soon", Summary: "近(臺南分局)", Start: "2026-10-22T08:00:00+08:00", Description: "進廠人員:羅羿昇"},   // 2 days
		{ID: "today", Summary: "今(臺南分局)", Start: "2026-10-21", AllDay: true, Description: "進廠人員:羅羿昇"},   // tomorrow
		{ID: "started", Summary: "已(臺南分局)", Start: "2026-10-20", AllDay: true, Description: "進廠人員:羅羿昇"}, // started today: too late
	}
	e.g.events["bip@group"] = nil
	_, out := previewWithDirectory(t, e)
	want := map[string]struct {
		due  bool
		days int
		from string
	}{
		"far": {false, 8, "2026-10-21"}, "edge": {true, 7, "2026-10-20"}, "soon": {true, 2, "2026-10-15"},
		"today": {true, 1, "2026-10-14"}, "started": {false, 0, "2026-10-13"},
	}
	for id, w := range want {
		ev := out.event(id)
		if ev.NoticeDue != w.due || ev.DaysUntil != w.days || ev.NoticeFrom != w.from {
			t.Errorf("%s: due=%v days=%d from=%s, want %+v", id, ev.NoticeDue, ev.DaysUntil, ev.NoticeFrom, w)
		}
	}
}

func TestPreview_UsesTaipeiCalendarDaysNotUTC(t *testing.T) {
	// 17:00 UTC on Oct 19 is already 01:00 on Oct 20 in Taipei.
	e := previewEnv(t)
	e.now = time.Date(2026, 10, 19, 17, 0, 0, 0, time.UTC)
	e.dir.users = directoryUsers()
	e.g.events["ida@group"] = []google.Event{{ID: "x", Summary: "x(臺南分局)", Start: "2026-10-27", AllDay: true, Description: "進廠人員:羅羿昇"}}
	e.g.events["bip@group"] = nil
	if ev := func() (r struct {
		ID         string     `json:"id"`
		Target     bool       `json:"target_location"`
		NoticeDue  bool       `json:"notice_due"`
		NoticeFrom string     `json:"notice_from"`
		DaysUntil  int        `json:"days_until"`
		Candidates []candJSON `json:"candidates"`
	}) {
		_, o := previewWithDirectory(t, e)
		return o.event("x")
	}(); ev.DaysUntil != 7 || !ev.NoticeDue {
		t.Errorf("in Taipei it is Oct 20, so Oct 27 is 7 days away and due: %+v", ev)
	}
}

func TestPreview_WithoutADirectoryStillShowsEventsAndSaysSo(t *testing.T) {
	// not configured
	e := previewEnv(t)
	e.dir.err = ErrDirectoryNotConfigured
	rec, out := previewWithDirectory(t, e)
	if rec.Code != 200 || out.Directory.Configured || len(out.Events) != 5 {
		t.Fatalf("%d %+v events=%d", rec.Code, out.Directory, len(out.Events))
	}
	for _, ev := range out.Events {
		if len(ev.Candidates) != 0 {
			t.Errorf("%s: without a directory attendees must not be reported as 'not found': %+v", ev.ID, ev.Candidates)
		}
	}

	// configured but failing: events still come back, with the reason
	e = previewEnv(t)
	e.dir.err = errors.New("目錄服務拒絕了 API key")
	rec, out = previewWithDirectory(t, e)
	if rec.Code != 200 || !out.Directory.Configured || !strings.Contains(out.Directory.Error, "API key") || len(out.Events) != 5 {
		t.Errorf("%d %+v", rec.Code, out.Directory)
	}

	// no directory wired at all
	e = previewEnv(t)
	e.c.directory = nil
	if rec, out = previewWithDirectory(t, e); rec.Code != 200 || out.Directory.Configured {
		t.Errorf("nil directory: %d %+v", rec.Code, out.Directory)
	}
}

func TestPreview_LooksUpTheDirectoryOncePerRequest(t *testing.T) {
	e := previewEnv(t)
	e.dir.users = directoryUsers()
	previewWithDirectory(t, e)
	if e.dir.calls != 1 {
		t.Errorf("the directory should be fetched once per preview, not once per event: %d", e.dir.calls)
	}
}
