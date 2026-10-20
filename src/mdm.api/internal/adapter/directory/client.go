// Package directory reads the company user directory (AD-backed) through its
// internal HTTP API: GET {base}/api/users with an X-API-Key header, returning
// who works here and, where filled in, whether they hold a driver's license
// and own a car.
package directory

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

type User struct {
	SamAccountName string
	DisplayName    string
	Mail           string
	Department     string
	Enabled        bool
	// HasDriverLicense / HasOwnCar are tri-state: nil = not recorded yet.
	HasDriverLicense *bool
	HasOwnCar        *bool
}

type Client struct{ HTTP *http.Client }

func NewClient() *Client {
	return &Client{HTTP: &http.Client{
		Timeout: 15 * time.Second,
		// Never follow a redirect. net/http only strips Authorization/Cookie on
		// a cross-host redirect, not a custom header like X-API-Key, so a
		// redirecting (or compromised) endpoint could otherwise carry the key
		// to another host.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

var (
	ErrBaseURLInvalid = errors.New("目錄服務網址格式不正確（需要 http:// 或 https:// 開頭的網址）")
	ErrUnauthorized   = errors.New("目錄服務拒絕了 API key（HTTP 401/403），請確認 API key 是否正確")
	ErrIncomplete     = errors.New("目錄服務回傳的使用者不完整（count 與實際筆數不符，可能被分頁或截斷）")
)

// NormalizeBaseURL accepts what an admin is likely to paste — "http://10.6.20.14:8089",
// with or without a trailing slash, or the full ".../api/users" endpoint — and
// returns the base, rejecting anything that isn't a plain http(s) URL (no
// credentials in the URL, no query or fragment).
func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", ErrBaseURLInvalid
	}
	path := strings.TrimRight(u.Path, "/")
	path = strings.TrimSuffix(path, "/api/users")
	return u.Scheme + "://" + u.Host + strings.TrimRight(path, "/"), nil
}

// ListUsers fetches the whole directory. Errors never contain the API key or
// the request URL.
func (c *Client) ListUsers(ctx context.Context, baseURL, apiKey string) ([]User, error) {
	base, err := NormalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/users", nil)
	if err != nil {
		return nil, ErrBaseURLInvalid
	}
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // drop the URL net/http embeds
		}
		return nil, fmt.Errorf("無法連線到目錄服務：%w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, ErrUnauthorized
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("目錄服務回應錯誤（HTTP %d）", resp.StatusCode)
	}

	var parsed struct {
		Count *int `json:"count"`
		Users []struct {
			SamAccountName   string  `json:"samAccountName"`
			DisplayName      *string `json:"displayName"`
			Mail             *string `json:"mail"`
			Department       *string `json:"department"`
			Enabled          bool    `json:"enabled"`
			HasDriverLicense *bool   `json:"hasDriverLicense"`
			HasOwnCar        *bool   `json:"hasOwnCar"`
		} `json:"users"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Users == nil {
		return nil, errors.New("目錄服務回傳的格式不是預期的 JSON（缺少 users）")
	}
	if parsed.Count != nil && *parsed.Count != len(parsed.Users) {
		return nil, ErrIncomplete
	}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return strings.TrimSpace(*p)
	}
	out := make([]User, 0, len(parsed.Users))
	for _, u := range parsed.Users {
		out = append(out, User{
			SamAccountName: u.SamAccountName, DisplayName: str(u.DisplayName), Mail: str(u.Mail),
			Department: str(u.Department), Enabled: u.Enabled,
			HasDriverLicense: u.HasDriverLicense, HasOwnCar: u.HasOwnCar,
		})
	}
	return out, nil
}
