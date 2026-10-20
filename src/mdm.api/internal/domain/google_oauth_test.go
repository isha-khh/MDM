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
