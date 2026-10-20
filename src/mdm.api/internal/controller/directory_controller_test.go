package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/mdm-server/internal/adapter/directory"
	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/middleware"
)

type fakeDirRepo struct{ s domain.DirectorySettings }

func (f *fakeDirRepo) Get(context.Context) (*domain.DirectorySettings, error) {
	c := f.s
	return &c, nil
}
func (f *fakeDirRepo) Save(_ context.Context, base, enc, by string) error {
	f.s.BaseURL, f.s.APIKeyEnc, f.s.UpdatedBy, f.s.UpdatedAt = base, enc, &by, time.Now()
	return nil
}
func (f *fakeDirRepo) Clear(_ context.Context, by string) error {
	f.s = domain.DirectorySettings{UpdatedBy: &by}
	return nil
}

type fakeDirClient struct {
	users           []directory.User
	err             error
	gotBase, gotKey string
	calls           int
}

func (f *fakeDirClient) ListUsers(_ context.Context, base, key string) ([]directory.User, error) {
	f.calls++
	f.gotBase, f.gotKey = base, key
	return f.users, f.err
}

func bptr(b bool) *bool { return &b }

type dirEnv struct {
	c    *DirectoryController
	repo *fakeDirRepo
	cl   *fakeDirClient
	mux  *http.ServeMux
}

func newDirEnv(t *testing.T, key string) *dirEnv {
	t.Helper()
	e := &dirEnv{repo: &fakeDirRepo{}, cl: &fakeDirClient{}}
	e.c = NewDirectoryController(e.repo, key, middleware.NewAuthHelper(testJWTSecret, nil), e.cl)
	e.mux = http.NewServeMux()
	e.c.RegisterRoutes(e.mux)
	return e
}

const dirPath = "/api/settings/directory"

func (e *dirEnv) put(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	return do(e.mux, "PUT", dirPath, tokenFor(t, "admin", "sys_admin"), "application/json", []byte(body))
}

func TestDirectory_SaveEncryptsTheKeyAndNeverReturnsIt(t *testing.T) {
	e := newDirEnv(t, validKey())
	rec := e.put(t, `{"base_url":" http://10.6.20.14:8089/api/users ","api_key":"  THE-API-KEY  "}`)
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	if e.repo.s.BaseURL != "http://10.6.20.14:8089" {
		t.Errorf("base url should be normalised: %q", e.repo.s.BaseURL)
	}
	if strings.Contains(e.repo.s.APIKeyEnc, "THE-API-KEY") || !strings.HasPrefix(e.repo.s.APIKeyEnc, "v1:") {
		t.Errorf("api key not encrypted at rest: %q", e.repo.s.APIKeyEnc)
	}
	if got, err := e.c.box.Decrypt(e.repo.s.APIKeyEnc, purposeDirectoryAPIKey); err != nil || got != "THE-API-KEY" {
		t.Errorf("decrypts to %q, %v (the key should be trimmed)", got, err)
	}
	if _, err := e.c.box.Decrypt(e.repo.s.APIKeyEnc, purposeGoogleClientSecret); err == nil {
		t.Error("the ciphertext must be bound to its purpose")
	}
	var st directoryStatus
	json.Unmarshal(rec.Body.Bytes(), &st)
	if st.BaseURL != "http://10.6.20.14:8089" || !st.HasAPIKey || strings.Contains(rec.Body.String(), "THE-API-KEY") || strings.Contains(rec.Body.String(), e.repo.s.APIKeyEnc) {
		t.Errorf("response: %s", rec.Body)
	}
	// GET never returns it either.
	if rec := do(e.mux, "GET", dirPath, tokenFor(t, "admin", "sys_admin"), "", nil); strings.Contains(rec.Body.String(), "THE-API-KEY") || strings.Contains(rec.Body.String(), "v1:") {
		t.Errorf("GET leaks the key: %s", rec.Body)
	}
}

func TestDirectory_BlankKeyKeepsTheStoredOneButFirstSaveNeedsOne(t *testing.T) {
	e := newDirEnv(t, validKey())
	if rec := e.put(t, `{"base_url":"http://10.6.20.14:8089"}`); rec.Code != 400 || e.repo.s.BaseURL != "" {
		t.Errorf("first save without a key: %d, stored %+v", rec.Code, e.repo.s)
	}
	e.put(t, `{"base_url":"http://10.6.20.14:8089","api_key":"K1"}`)
	before := e.repo.s.APIKeyEnc
	if rec := e.put(t, `{"base_url":"http://10.6.20.99:9000"}`); rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if e.repo.s.APIKeyEnc != before || e.repo.s.BaseURL != "http://10.6.20.99:9000" {
		t.Errorf("changing only the endpoint must keep the key: %+v", e.repo.s)
	}
}

func TestDirectory_RejectsBadInput(t *testing.T) {
	e := newDirEnv(t, validKey())
	for name, body := range map[string]string{
		"not a url":          `{"base_url":"10.6.20.14:8089","api_key":"k"}`,
		"file scheme":        `{"base_url":"file:///etc/passwd","api_key":"k"}`,
		"credentials in url": `{"base_url":"http://u:p@host","api_key":"k"}`,
		"empty":              `{"base_url":"","api_key":"k"}`,
		"huge key":           `{"base_url":"http://h","api_key":"` + strings.Repeat("k", 600) + `"}`,
		"garbage":            `nope`,
	} {
		if rec := e.put(t, body); rec.Code != 400 {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
		if e.repo.s.BaseURL != "" || e.repo.s.APIKeyEnc != "" {
			t.Errorf("%s: something was stored: %+v", name, e.repo.s)
		}
	}
}

func TestDirectory_NoEncryptionKeyMeansNoStorage(t *testing.T) {
	e := newDirEnv(t, "")
	rec := e.put(t, `{"base_url":"http://h","api_key":"k"}`)
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "SETTINGS_ENCRYPTION_KEY") || e.repo.s.APIKeyEnc != "" {
		t.Errorf("%d %s stored %+v", rec.Code, rec.Body, e.repo.s)
	}
}

func TestDirectory_RequiresSysAdmin(t *testing.T) {
	e := newDirEnv(t, validKey())
	for _, c := range []struct{ method, path string }{
		{"GET", dirPath}, {"PUT", dirPath}, {"DELETE", dirPath}, {"POST", dirPath + "/test"},
	} {
		for name, tok := range map[string]string{"anonymous": "", "ordinary user": tokenFor(t, "user", "")} {
			if rec := do(e.mux, c.method, c.path, tok, "application/json", []byte(`{"base_url":"http://h","api_key":"k"}`)); rec.Code != 401 {
				t.Errorf("%s %s as %s: %d", c.method, c.path, name, rec.Code)
			}
		}
	}
	if e.repo.s.BaseURL != "" || e.cl.calls != 0 {
		t.Error("an unauthorised request had an effect")
	}
}

func TestDirectory_Delete(t *testing.T) {
	e := newDirEnv(t, validKey())
	e.put(t, `{"base_url":"http://h","api_key":"k"}`)
	if rec := do(e.mux, "DELETE", dirPath, tokenFor(t, "admin", "sys_admin"), "", nil); rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if e.repo.s.BaseURL != "" || e.repo.s.APIKeyEnc != "" {
		t.Errorf("not cleared: %+v", e.repo.s)
	}
}

func TestDirectory_UsersUsesTheDecryptedKeyAndMapsFields(t *testing.T) {
	e := newDirEnv(t, validKey())
	if _, err := e.c.Users(context.Background()); !errors.Is(err, ErrDirectoryNotConfigured) {
		t.Errorf("before configuring: %v", err)
	}
	e.put(t, `{"base_url":"http://10.6.20.14:8089","api_key":"THE-API-KEY"}`)
	e.cl.users = []directory.User{
		{SamAccountName: "a", DisplayName: "26安環處-高雄-甲", Mail: "a@x", Enabled: true, HasDriverLicense: bptr(true), HasOwnCar: bptr(false)},
		{SamAccountName: "b", DisplayName: "乙", Enabled: false},
	}
	users, err := e.c.Users(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if e.cl.gotBase != "http://10.6.20.14:8089" || e.cl.gotKey != "THE-API-KEY" {
		t.Errorf("client got base=%q key=%q", e.cl.gotBase, e.cl.gotKey)
	}
	if len(users) != 2 || users[0].Disabled || !users[1].Disabled || users[0].HasOwnCar == nil || *users[0].HasOwnCar || users[1].HasDriverLicense != nil {
		t.Errorf("mapping: %+v", users)
	}
}

func TestDirectory_UsersFailsCleanlyWhenTheKeyChanged(t *testing.T) {
	e := newDirEnv(t, validKey())
	e.put(t, `{"base_url":"http://h","api_key":"k"}`)
	other := NewDirectoryController(e.repo, "ZmFrZS1vdGhlci1rZXktZmFrZS1vdGhlci1rZXktMTIzNDU=", middleware.NewAuthHelper(testJWTSecret, nil), e.cl)
	if other.box == nil {
		t.Skip("test key not accepted")
	}
	if _, err := other.Users(context.Background()); err == nil || e.cl.calls != 0 {
		t.Errorf("a key that can't decrypt must not reach the network: err=%v calls=%d", err, e.cl.calls)
	}
}

func TestDirectory_TestEndpointSummarisesWithoutExposingPeople(t *testing.T) {
	e := newDirEnv(t, validKey())
	admin := tokenFor(t, "admin", "sys_admin")
	if rec := do(e.mux, "POST", dirPath+"/test", admin, "", nil); rec.Code != 400 {
		t.Errorf("not configured: %d", rec.Code)
	}
	e.put(t, `{"base_url":"http://h","api_key":"k"}`)
	e.cl.users = []directory.User{
		{SamAccountName: "svc", Enabled: true}, // service account: no mail
		{SamAccountName: "a", DisplayName: "甲", Mail: "a@x", Enabled: true, HasDriverLicense: bptr(true), HasOwnCar: bptr(false)},
		{SamAccountName: "b", DisplayName: "乙", Mail: "b@x", Enabled: true, HasDriverLicense: bptr(true), HasOwnCar: bptr(true)},
		{SamAccountName: "c", DisplayName: "丙", Mail: "c@x", Enabled: true}, // nothing filled in
		{SamAccountName: "d", DisplayName: "丁", Mail: "d@x", Enabled: true, HasDriverLicense: bptr(false), HasOwnCar: bptr(false)},
		{SamAccountName: "off", DisplayName: "離職", Mail: "o@x", Enabled: false, HasDriverLicense: bptr(true)}, // disabled: not counted as a person
	}
	rec := do(e.mux, "POST", dirPath+"/test", admin, "", nil)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var out struct {
		Total, Enabled, People int
		License, Car           tri
	}
	var raw map[string]json.RawMessage
	json.Unmarshal(rec.Body.Bytes(), &raw)
	json.Unmarshal(raw["total_accounts"], &out.Total)
	json.Unmarshal(raw["enabled_accounts"], &out.Enabled)
	json.Unmarshal(raw["people"], &out.People)
	json.Unmarshal(raw["has_license"], &out.License)
	json.Unmarshal(raw["has_own_car"], &out.Car)
	if out.Total != 6 || out.Enabled != 5 || out.People != 4 {
		t.Errorf("counts: %+v", out)
	}
	if out.License != (tri{Yes: 2, No: 1, Unknown: 1}) || out.Car != (tri{Yes: 1, No: 2, Unknown: 1}) {
		t.Errorf("license %+v car %+v", out.License, out.Car)
	}
	for _, leak := range []string{"@x", "甲", "THE", "svc"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("the summary must not contain people's details (%q): %s", leak, rec.Body)
		}
	}
}

func TestDirectory_TestEndpointFailureIsBadGatewayNotACrash(t *testing.T) {
	e := newDirEnv(t, validKey())
	e.put(t, `{"base_url":"http://h","api_key":"SECRET-KEY"}`)
	e.cl.err = directory.ErrUnauthorized
	rec := do(e.mux, "POST", dirPath+"/test", tokenFor(t, "admin", "sys_admin"), "", nil)
	if rec.Code != 502 || !strings.Contains(rec.Body.String(), "API key") || strings.Contains(rec.Body.String(), "SECRET-KEY") {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}
