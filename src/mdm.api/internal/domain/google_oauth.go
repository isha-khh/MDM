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
	// RedirectURIs are the callback URLs registered for this client in Google
	// Cloud Console (taken from the uploaded JSON). A connect request is only
	// allowed to use one of these.
	RedirectURIs     []string
	ConnectedAccount string // the authorised Google account's address, shown in the UI
	UpdatedAt        time.Time
	UpdatedBy        *string
}

// GoogleCalendarCallbackPath is where Google sends the admin back after they
// authorise; the full redirect URI is the page's origin plus this path.
const GoogleCalendarCallbackPath = "/api/settings/google-calendar/callback"

// GoogleOAuthClient is what we need out of the JSON file Google Cloud Console
// offers to download after creating an OAuth client.
type GoogleOAuthClient struct {
	ClientID     string
	ClientSecret string
	RedirectURIs []string
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
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURIs []string `json:"redirect_uris"`
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
	var uris []string
	for _, u := range app.RedirectURIs {
		if u = strings.TrimSpace(u); u != "" {
			uris = append(uris, u)
		}
	}
	return &GoogleOAuthClient{ClientID: id, ClientSecret: secret, RedirectURIs: uris}, nil
}

var (
	ErrRedirectOriginInvalid  = errors.New("無法判斷目前的網站網址")
	ErrRedirectNotRegistered  = errors.New("目前網址的回呼路徑沒有在 Google 憑證的「已授權的重新導向 URI」中，請到 Google Cloud Console 加入後重新下載並上傳 JSON")
	ErrRedirectNoneRegistered = errors.New("上傳的憑證沒有任何「已授權的重新導向 URI」，請到 Google Cloud Console 加入後重新下載並上傳 JSON")
)

// PickGoogleRedirectURI builds the redirect URI for a connect request from the
// page's origin the admin is on (https://mdm.isha.net), and returns it only if
// it is one of the URIs registered for the client — Google would reject
// anything else with redirect_uri_mismatch, and this also stops a forged
// origin from steering the authorisation code somewhere unregistered. The
// browser's own origin is used rather than guessed from proxy headers, which
// differ per deployment (TLS terminated in front of nginx, etc.).
func PickGoogleRedirectURI(registered []string, origin string) (string, error) {
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	if !strings.HasPrefix(origin, "https://") && !strings.HasPrefix(origin, "http://") || strings.ContainsAny(origin, "?# ") {
		return "", ErrRedirectOriginInvalid
	}
	if len(registered) == 0 {
		return "", ErrRedirectNoneRegistered
	}
	want := origin + GoogleCalendarCallbackPath
	for _, r := range registered {
		if r == want {
			return want, nil
		}
	}
	return "", ErrRedirectNotRegistered
}
