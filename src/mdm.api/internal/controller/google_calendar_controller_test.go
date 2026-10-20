package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/middleware"
)

const testJWTSecret = "test-jwt-secret"

// fakeGoogleRepo is an in-memory stand-in holding exactly what the real table
// would: ciphertext only, so tests can assert on what actually gets stored.
type fakeGoogleRepo struct{ s domain.GoogleCalendarSettings }

func (f *fakeGoogleRepo) Get(context.Context) (*domain.GoogleCalendarSettings, error) {
	c := f.s
	return &c, nil
}
func (f *fakeGoogleRepo) SaveClient(_ context.Context, id, enc, by string) error {
	f.s.ClientID, f.s.ClientSecretEnc, f.s.RefreshTokenEnc = id, enc, ""
	f.s.UpdatedAt, f.s.UpdatedBy = time.Now(), &by
	return nil
}
func (f *fakeGoogleRepo) Clear(_ context.Context, by string) error {
	f.s = domain.GoogleCalendarSettings{UpdatedAt: time.Now(), UpdatedBy: &by}
	return nil
}

func validKey() string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)) }

const oauthJSON = `{"web":{"client_id":"123-abc.apps.googleusercontent.com","client_secret":"GOCSPX-TOPSECRET","redirect_uris":["https://mdm.isha.net/api/settings/google-calendar/callback"]}}`

func newGoogleServer(t *testing.T, key string) (*GoogleCalendarController, *fakeGoogleRepo, *http.ServeMux) {
	t.Helper()
	repo := &fakeGoogleRepo{}
	c := NewGoogleCalendarController(repo, key, middleware.NewAuthHelper(testJWTSecret, nil))
	mux := http.NewServeMux()
	c.RegisterRoutes(mux)
	return c, repo, mux
}

func tokenFor(t *testing.T, role, systemRole string) string {
	t.Helper()
	access, _, _, err := middleware.GenerateTokens(testJWTSecret, "user-1", "tester", role, systemRole)
	if err != nil {
		t.Fatal(err)
	}
	return access
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

const credsPath = "/api/settings/google-calendar/credentials"

func TestUpload_EncryptsSecretAtRestAndNeverReturnsIt(t *testing.T) {
	c, repo, mux := newGoogleServer(t, validKey())
	body, ct := multipartBody(t, "file", oauthJSON)

	rec := do(mux, "POST", credsPath, tokenFor(t, "admin", "sys_admin"), ct, body)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	// What reached the database is ciphertext, not the secret.
	if repo.s.ClientID != "123-abc.apps.googleusercontent.com" {
		t.Errorf("client id: %q", repo.s.ClientID)
	}
	if strings.Contains(repo.s.ClientSecretEnc, "TOPSECRET") || !strings.HasPrefix(repo.s.ClientSecretEnc, "v1:") {
		t.Errorf("secret not encrypted at rest: %q", repo.s.ClientSecretEnc)
	}
	// ...and it decrypts back to the original with the configured key.
	if got, err := c.clientSecret(&repo.s); err != nil || got != "GOCSPX-TOPSECRET" {
		t.Errorf("decrypt: %q, %v", got, err)
	}
	// The response confirms the state without echoing the secret or ciphertext.
	if strings.Contains(rec.Body.String(), "TOPSECRET") || strings.Contains(rec.Body.String(), repo.s.ClientSecretEnc) {
		t.Errorf("response leaks the secret: %s", rec.Body)
	}
	var st googleCalendarStatus
	json.Unmarshal(rec.Body.Bytes(), &st)
	if !st.HasClientSecret || st.Connected || st.ClientID == "" {
		t.Errorf("unexpected status %+v", st)
	}
}

func TestUpload_AcceptsRawJSONBody(t *testing.T) {
	_, repo, mux := newGoogleServer(t, validKey())
	rec := do(mux, "POST", credsPath, tokenFor(t, "admin", "sys_admin"), "application/json", []byte(oauthJSON))
	if rec.Code != 200 || repo.s.ClientSecretEnc == "" {
		t.Errorf("status %d, stored %q: %s", rec.Code, repo.s.ClientSecretEnc, rec.Body)
	}
}

func TestUpload_ReplacingCredentialsDropsOldRefreshToken(t *testing.T) {
	_, repo, mux := newGoogleServer(t, validKey())
	repo.s.RefreshTokenEnc = "v1:old-token-for-the-previous-client"
	rec := do(mux, "POST", credsPath, tokenFor(t, "admin", "sys_admin"), "application/json", []byte(oauthJSON))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if repo.s.RefreshTokenEnc != "" {
		t.Error("a token issued to the old client must not survive new credentials")
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
		_, repo, mux := newGoogleServer(t, validKey())
		rec := do(mux, "POST", credsPath, tokenFor(t, "admin", "sys_admin"), "application/json", []byte(in))
		if rec.Code != 400 {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
		if repo.s.ClientID != "" || repo.s.ClientSecretEnc != "" {
			t.Errorf("%s: something was stored: %+v", name, repo.s)
		}
	}
}

func TestUpload_MultipartWithoutFileFieldIs400(t *testing.T) {
	_, _, mux := newGoogleServer(t, validKey())
	body, ct := multipartBody(t, "wrong_field", oauthJSON)
	if rec := do(mux, "POST", credsPath, tokenFor(t, "admin", "sys_admin"), ct, body); rec.Code != 400 {
		t.Errorf("status %d, want 400", rec.Code)
	}
}

func TestUpload_OversizedBodyIsRejected(t *testing.T) {
	_, repo, mux := newGoogleServer(t, validKey())
	big := `{"web":{"client_id":"x","client_secret":"` + strings.Repeat("a", 100<<10) + `"}}`
	if rec := do(mux, "POST", credsPath, tokenFor(t, "admin", "sys_admin"), "application/json", []byte(big)); rec.Code != 400 {
		t.Errorf("status %d, want 400", rec.Code)
	}
	if repo.s.ClientSecretEnc != "" {
		t.Error("oversized upload must not be stored")
	}
}

func TestUpload_RefusesToStorePlaintextWithoutAKey(t *testing.T) {
	for name, key := range map[string]string{"unset": "", "malformed": "change-me-in-production"} {
		_, repo, mux := newGoogleServer(t, key)
		rec := do(mux, "POST", credsPath, tokenFor(t, "admin", "sys_admin"), "application/json", []byte(oauthJSON))
		if rec.Code != 503 {
			t.Errorf("%s: status %d, want 503", name, rec.Code)
		}
		if repo.s.ClientSecretEnc != "" || repo.s.ClientID != "" {
			t.Errorf("%s: nothing may be stored without a valid key: %+v", name, repo.s)
		}
		if !strings.Contains(rec.Body.String(), "SETTINGS_ENCRYPTION_KEY") {
			t.Errorf("%s: message should name the setting: %s", name, rec.Body)
		}
	}
}

func TestEndpoints_RequireSysAdmin(t *testing.T) {
	_, repo, mux := newGoogleServer(t, validKey())
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/settings/google-calendar"},
		{"DELETE", "/api/settings/google-calendar"},
		{"POST", credsPath},
	} {
		// No token, and a signed-in ordinary user.
		for name, tok := range map[string]string{"anonymous": "", "ordinary user": tokenFor(t, "user", "")} {
			rec := do(mux, c.method, c.path, tok, "application/json", []byte(oauthJSON))
			if rec.Code != 401 {
				t.Errorf("%s %s as %s: status %d, want 401", c.method, c.path, name, rec.Code)
			}
		}
	}
	if repo.s.ClientSecretEnc != "" {
		t.Error("an unauthorised upload was stored")
	}
}

func TestStatusAndDelete(t *testing.T) {
	_, repo, mux := newGoogleServer(t, validKey())
	admin := tokenFor(t, "admin", "sys_admin")

	rec := do(mux, "GET", "/api/settings/google-calendar", admin, "", nil)
	var st googleCalendarStatus
	json.Unmarshal(rec.Body.Bytes(), &st)
	if rec.Code != 200 || !st.EncryptionReady || st.HasClientSecret || st.Connected {
		t.Errorf("fresh state: %d %+v", rec.Code, st)
	}

	do(mux, "POST", credsPath, admin, "application/json", []byte(oauthJSON))
	repo.s.RefreshTokenEnc = "v1:whatever"
	rec = do(mux, "GET", "/api/settings/google-calendar", admin, "", nil)
	json.Unmarshal(rec.Body.Bytes(), &st)
	if !st.HasClientSecret || !st.Connected || strings.Contains(rec.Body.String(), "whatever") {
		t.Errorf("after upload: %+v body=%s", st, rec.Body)
	}

	if rec := do(mux, "DELETE", "/api/settings/google-calendar", admin, "", nil); rec.Code != 200 {
		t.Fatalf("delete status %d", rec.Code)
	}
	if repo.s.ClientID != "" || repo.s.ClientSecretEnc != "" || repo.s.RefreshTokenEnc != "" {
		t.Errorf("delete should wipe everything: %+v", repo.s)
	}
}

func TestStatusReportsWhyEncryptionIsOff(t *testing.T) {
	_, _, mux := newGoogleServer(t, "")
	rec := do(mux, "GET", "/api/settings/google-calendar", tokenFor(t, "admin", "sys_admin"), "", nil)
	var st googleCalendarStatus
	json.Unmarshal(rec.Body.Bytes(), &st)
	if st.EncryptionReady || st.EncryptionError == "" {
		t.Errorf("should say encryption isn't ready and why: %+v", st)
	}
}

func TestDecryptWithDifferentKeyFails(t *testing.T) {
	// A restart with a changed/lost key can't silently produce garbage.
	c1, repo, mux := newGoogleServer(t, validKey())
	do(mux, "POST", credsPath, tokenFor(t, "admin", "sys_admin"), "application/json", []byte(oauthJSON))
	_ = c1
	otherKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	c2 := NewGoogleCalendarController(repo, otherKey, middleware.NewAuthHelper(testJWTSecret, nil))
	if _, err := c2.clientSecret(&repo.s); err == nil {
		t.Error("decrypting with a different key must fail")
	}
}
