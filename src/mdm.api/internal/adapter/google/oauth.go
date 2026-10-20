// Package google talks to Google's OAuth 2.0 and Calendar endpoints with
// plain net/http: authorise (code flow, offline access), exchange/refresh
// tokens, revoke, and list the calendars an account can see.
package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ReadOnlyCalendarScope is all this integration ever asks for.
const ReadOnlyCalendarScope = "https://www.googleapis.com/auth/calendar.readonly"

type Client struct {
	HTTP        *http.Client
	AuthURL     string
	TokenURL    string
	RevokeURL   string
	CalendarAPI string
}

func NewClient() *Client {
	return &Client{
		HTTP:        &http.Client{Timeout: 20 * time.Second},
		AuthURL:     "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:    "https://oauth2.googleapis.com/token",
		RevokeURL:   "https://oauth2.googleapis.com/revoke",
		CalendarAPI: "https://www.googleapis.com/calendar/v3",
	}
}

type Token struct {
	AccessToken  string
	RefreshToken string // empty if Google didn't issue one
	ExpiresIn    int
}

type Calendar struct {
	ID         string `json:"id"`
	Summary    string `json:"summary"`
	AccessRole string `json:"access_role"`
	Primary    bool   `json:"primary"`
}

// APIError is an error response from Google. It carries Google's short error
// code and description only — never the request, which holds the secret.
type APIError struct {
	Status      int
	Code        string // e.g. "invalid_grant", "redirect_uri_mismatch"
	Description string
}

func (e *APIError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("google: %s (%s)", e.Code, e.Description)
	}
	return fmt.Sprintf("google: %s (HTTP %d)", e.Code, e.Status)
}

// IsInvalidGrant reports that a refresh token / code is no longer valid
// (revoked, expired, or issued to a different client) — the fix is to
// authorise again, not to retry.
func IsInvalidGrant(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Code == "invalid_grant"
}

// AuthCodeURL is where to send the admin. access_type=offline together with
// prompt=consent makes Google issue a refresh token every time, including when
// the account has authorised this client before (otherwise it is only issued
// on the very first consent, and a reconnect would silently get none).
func (c *Client) AuthCodeURL(clientID, redirectURI, state string) string {
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", ReadOnlyCalendarScope)
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	q.Set("state", state)
	return c.AuthURL + "?" + q.Encode()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func (c *Client) postForm(ctx context.Context, endpoint string, form url.Values) (*tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, redactTransportErr(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tr tokenResponse
	_ = json.Unmarshal(body, &tr)
	if resp.StatusCode != http.StatusOK || tr.Error != "" {
		code := tr.Error
		if code == "" {
			code = "http_error"
		}
		return nil, &APIError{Status: resp.StatusCode, Code: code, Description: tr.ErrorDesc}
	}
	return &tr, nil
}

// redactTransportErr keeps net/http's *url.Error (which embeds the full
// request URL) out of anything that gets logged or shown.
func redactTransportErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("無法連線到 Google：%w", ue.Err)
	}
	return err
}

func (c *Client) ExchangeCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*Token, error) {
	tr, err := c.postForm(ctx, c.TokenURL, url.Values{
		"code":          {code},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	})
	if err != nil {
		return nil, err
	}
	return &Token{AccessToken: tr.AccessToken, RefreshToken: tr.RefreshToken, ExpiresIn: tr.ExpiresIn}, nil
}

func (c *Client) RefreshAccessToken(ctx context.Context, clientID, clientSecret, refreshToken string) (string, error) {
	tr, err := c.postForm(ctx, c.TokenURL, url.Values{
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"grant_type":    {"refresh_token"},
	})
	if err != nil {
		return "", err
	}
	if tr.AccessToken == "" {
		return "", &APIError{Status: http.StatusOK, Code: "no_access_token"}
	}
	return tr.AccessToken, nil
}

// Revoke invalidates a token at Google. Callers treat failure as best-effort.
func (c *Client) Revoke(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.RevokeURL, strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return redactTransportErr(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return &APIError{Status: resp.StatusCode, Code: "revoke_failed"}
	}
	return nil
}

// ListCalendars returns every calendar on the account's list — its own and
// the ones it is subscribed to — following pagination.
func (c *Client) ListCalendars(ctx context.Context, accessToken string) ([]Calendar, error) {
	var out []Calendar
	pageToken := ""
	for page := 0; page < 20; page++ { // hard stop against a pagination loop
		q := url.Values{"maxResults": {"250"}, "minAccessRole": {"freeBusyReader"}}
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.CalendarAPI+"/users/me/calendarList?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, redactTransportErr(err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()

		var parsed struct {
			Items []struct {
				ID         string `json:"id"`
				Summary    string `json:"summary"`
				SummaryOv  string `json:"summaryOverride"`
				AccessRole string `json:"accessRole"`
				Primary    bool   `json:"primary"`
			} `json:"items"`
			NextPageToken string `json:"nextPageToken"`
			Error         struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &parsed)
		if resp.StatusCode != http.StatusOK {
			code := parsed.Error.Status
			if code == "" {
				code = "http_error"
			}
			return nil, &APIError{Status: resp.StatusCode, Code: code, Description: parsed.Error.Message}
		}
		for _, it := range parsed.Items {
			name := it.SummaryOv // the user's own rename of a subscribed calendar wins
			if name == "" {
				name = it.Summary
			}
			out = append(out, Calendar{ID: it.ID, Summary: name, AccessRole: it.AccessRole, Primary: it.Primary})
		}
		if parsed.NextPageToken == "" {
			return out, nil
		}
		pageToken = parsed.NextPageToken
	}
	return out, nil
}
