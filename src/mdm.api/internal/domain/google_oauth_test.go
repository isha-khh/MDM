package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestParseGoogleOAuthClientJSON(t *testing.T) {
	web := `{"web":{"client_id":" 123-abc.apps.googleusercontent.com ","project_id":"p","auth_uri":"https://accounts.google.com/o/oauth2/auth","token_uri":"https://oauth2.googleapis.com/token","client_secret":"GOCSPX-secret","redirect_uris":["https://mdm.isha.net/api/settings/google-calendar/callback"],"javascript_origins":["https://mdm.isha.net"]}}`
	desktop := `{"installed":{"client_id":"456-def.apps.googleusercontent.com","client_secret":"GOCSPX-other","redirect_uris":["http://localhost"]}}`

	cases := []struct {
		name       string
		in         string
		wantID     string
		wantSecret string
		wantErr    error
	}{
		{"web application", web, "123-abc.apps.googleusercontent.com", "GOCSPX-secret", nil},
		{"desktop application", desktop, "456-def.apps.googleusercontent.com", "GOCSPX-other", nil},
		{"service account key", `{"type":"service_account","project_id":"p","private_key":"-----BEGIN PRIVATE KEY-----\nxx","client_email":"a@b.iam.gserviceaccount.com","client_id":"1"}`, "", "", ErrOAuthJSONServiceAccount},
		{"not json", `this is not json`, "", "", ErrOAuthJSONInvalid},
		{"empty input", ``, "", "", ErrOAuthJSONInvalid},
		{"json but not oauth", `{"hello":"world"}`, "", "", ErrOAuthJSONUnrecognized},
		{"empty web block", `{"web":{}}`, "", "", ErrOAuthJSONIncomplete},
		{"missing secret", `{"web":{"client_id":"x"}}`, "", "", ErrOAuthJSONIncomplete},
		{"missing id", `{"web":{"client_secret":"x"}}`, "", "", ErrOAuthJSONIncomplete},
		{"blank values", `{"web":{"client_id":"  ","client_secret":"\t"}}`, "", "", ErrOAuthJSONIncomplete},
	}
	for _, c := range cases {
		got, err := ParseGoogleOAuthClientJSON([]byte(c.in))
		if !errors.Is(err, c.wantErr) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.wantErr)
			continue
		}
		if c.wantErr == nil && (got.ClientID != c.wantID || got.ClientSecret != c.wantSecret) {
			t.Errorf("%s: got %+v", c.name, got)
		}
	}
}

func TestParseGoogleOAuthClientJSON_RedirectURIs(t *testing.T) {
	got, err := ParseGoogleOAuthClientJSON([]byte(`{"web":{"client_id":"a","client_secret":"b","redirect_uris":[" https://mdm.isha.net/cb ","","http://localhost:5173/cb"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.RedirectURIs) != 2 || got.RedirectURIs[0] != "https://mdm.isha.net/cb" || got.RedirectURIs[1] != "http://localhost:5173/cb" {
		t.Errorf("redirect uris: %q", got.RedirectURIs)
	}
	if got, _ := ParseGoogleOAuthClientJSON([]byte(`{"web":{"client_id":"a","client_secret":"b"}}`)); len(got.RedirectURIs) != 0 {
		t.Errorf("no redirect_uris should give none, got %q", got.RedirectURIs)
	}
}

func TestPickGoogleRedirectURI(t *testing.T) {
	reg := []string{
		"https://mdm.isha.net/api/settings/google-calendar/callback",
		"http://localhost:5173/api/settings/google-calendar/callback",
	}
	cases := []struct {
		name, origin string
		reg          []string
		want         string
		wantErr      error
	}{
		{"production", "https://mdm.isha.net", reg, reg[0], nil},
		{"trailing slash tolerated", "https://mdm.isha.net/", reg, reg[0], nil},
		{"surrounding spaces tolerated", "  http://localhost:5173 ", reg, reg[1], nil},
		{"origin not registered", "https://evil.example.com", reg, "", ErrRedirectNotRegistered},
		{"same host wrong scheme", "http://mdm.isha.net", reg, "", ErrRedirectNotRegistered},
		{"same host other port", "https://mdm.isha.net:8443", reg, "", ErrRedirectNotRegistered},
		{"lookalike suffix", "https://mdm.isha.net.evil.com", reg, "", ErrRedirectNotRegistered},
		{"nothing registered", "https://mdm.isha.net", nil, "", ErrRedirectNoneRegistered},
		{"empty origin", "", reg, "", ErrRedirectOriginInvalid},
		{"no scheme", "mdm.isha.net", reg, "", ErrRedirectOriginInvalid},
		{"javascript scheme", "javascript:alert(1)", reg, "", ErrRedirectOriginInvalid},
		{"origin carrying a path or query", "https://mdm.isha.net/x?y=1", reg, "", ErrRedirectOriginInvalid},
	}
	for _, c := range cases {
		got, err := PickGoogleRedirectURI(c.reg, c.origin)
		if !errors.Is(err, c.wantErr) || got != c.want {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", c.name, got, err, c.want, c.wantErr)
		}
	}
}

func TestParseGoogleOAuthClientJSON_ErrorsNeverLeakContents(t *testing.T) {
	// Error text is shown in the UI and logged, so it must not echo the file.
	secretFile := `{"web":{"client_id":"id-LEAKME","client_secret":"GOCSPX-LEAKME"}` // truncated: invalid JSON
	incomplete := `{"web":{"client_id":"id-LEAKME"}}`
	for _, in := range []string{secretFile, incomplete} {
		_, err := ParseGoogleOAuthClientJSON([]byte(in))
		if err == nil || strings.Contains(err.Error(), "LEAKME") {
			t.Errorf("error should exist and not contain file contents, got %v", err)
		}
	}
}
