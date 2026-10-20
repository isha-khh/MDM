package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// GoogleCalendarSettings is the stored Google Calendar connection. Secrets
// are kept only as ciphertext (see internal/secretbox); decrypting them is the
// caller's job, with the key from the environment.
type GoogleCalendarSettings struct {
	ClientID        string
	ClientSecretEnc string // empty until credentials are uploaded
	RefreshTokenEnc string // empty until the Google account has been authorised
	UpdatedAt       time.Time
	UpdatedBy       *string
}

// GoogleOAuthClient is what we need out of the JSON file Google Cloud Console
// offers to download after creating an OAuth client.
type GoogleOAuthClient struct {
	ClientID     string
	ClientSecret string
}

var (
	ErrOAuthJSONInvalid        = errors.New("不是有效的 JSON 檔")
	ErrOAuthJSONServiceAccount = errors.New("這是服務帳戶（service account）金鑰，不是 OAuth 用戶端 ID。請到「API 和服務 → 憑證」建立 OAuth 用戶端 ID 後再下載 JSON")
	ErrOAuthJSONUnrecognized   = errors.New("找不到 OAuth 用戶端資料（缺少 web 或 installed 區塊），請確認下載的是 OAuth 用戶端 ID 的 JSON")
	ErrOAuthJSONIncomplete     = errors.New("OAuth 用戶端 JSON 缺少 client_id 或 client_secret")
)

type googleOAuthFile struct {
	Type      string          `json:"type"`
	Web       *googleOAuthApp `json:"web"`
	Installed *googleOAuthApp `json:"installed"`
}

type googleOAuthApp struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// ParseGoogleOAuthClientJSON extracts the client ID and secret from Google's
// downloaded OAuth client file ({"web": {...}} for a web application,
// {"installed": {...}} for a desktop one). Errors never include the file's
// contents, since they get shown to the user and logged.
func ParseGoogleOAuthClientJSON(data []byte) (*GoogleOAuthClient, error) {
	var f googleOAuthFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, ErrOAuthJSONInvalid
	}
	if f.Type == "service_account" {
		return nil, ErrOAuthJSONServiceAccount
	}
	app := f.Web
	if app == nil {
		app = f.Installed
	}
	if app == nil {
		return nil, ErrOAuthJSONUnrecognized
	}
	id, secret := strings.TrimSpace(app.ClientID), strings.TrimSpace(app.ClientSecret)
	if id == "" || secret == "" {
		return nil, ErrOAuthJSONIncomplete
	}
	return &GoogleOAuthClient{ClientID: id, ClientSecret: secret}, nil
}
