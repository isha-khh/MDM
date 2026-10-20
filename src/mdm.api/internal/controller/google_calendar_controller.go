package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/mdm-server/internal/adapter/google"
	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/port"
	"github.com/anthropics/mdm-server/internal/secretbox"
	"github.com/anthropics/mdm-server/internal/service/tripcar"
)

// Purposes bind each ciphertext to what it is for (see secretbox).
const (
	purposeGoogleClientSecret = "google_calendar.client_secret"
	purposeGoogleRefreshToken = "google_calendar.refresh_token"
	purposeGoogleOAuthState   = "google_calendar.oauth_state"
)

// maxCredentialsFileBytes: a Google OAuth client JSON is well under 1KB; the
// cap just stops someone uploading something huge to this endpoint.
const maxCredentialsFileBytes = 64 << 10

// oauthStateTTL is how long the admin has to finish the Google consent screen.
const oauthStateTTL = 10 * time.Minute

// googleAPI is the slice of Google's APIs this controller uses; *google.Client
// satisfies it, tests substitute a fake.
type googleAPI interface {
	AuthCodeURL(clientID, redirectURI, state string) string
	ExchangeCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*google.Token, error)
	RefreshAccessToken(ctx context.Context, clientID, clientSecret, refreshToken string) (string, error)
	ListCalendars(ctx context.Context, accessToken string) ([]google.Calendar, error)
	ListEvents(ctx context.Context, accessToken, calendarID string, timeMin, timeMax time.Time) ([]google.Event, error)
	Revoke(ctx context.Context, token string) error
}

// GoogleCalendarController manages the Google Calendar connection: uploading
// the OAuth client credentials (encrypted at rest), authorising a Google
// account, testing the connection, and removing it all. Sys-admin only, like
// the mail settings.
type GoogleCalendarController struct {
	repo   port.GoogleCalendarSettingsRepository
	google googleAPI
	box    *secretbox.Box // nil when SETTINGS_ENCRYPTION_KEY is unset/invalid
	// boxErr says why box is nil, shown to the admin so the fix is obvious.
	boxErr string
	auth   *middleware.AuthHelper
	now    func() time.Time
}

// NewGoogleCalendarController takes the raw SETTINGS_ENCRYPTION_KEY value. An
// empty or malformed key doesn't stop the server (this is an optional
// feature); the endpoints just refuse to store secrets and say why.
func NewGoogleCalendarController(repo port.GoogleCalendarSettingsRepository, encryptionKey string, auth *middleware.AuthHelper, g googleAPI) *GoogleCalendarController {
	c := &GoogleCalendarController{repo: repo, auth: auth, google: g, now: time.Now}
	if strings.TrimSpace(encryptionKey) == "" {
		c.boxErr = "尚未設定 SETTINGS_ENCRYPTION_KEY，無法加密儲存憑證"
		log.Printf("[google-calendar] SETTINGS_ENCRYPTION_KEY is not set: credential storage disabled")
		return c
	}
	box, err := secretbox.NewFromString(encryptionKey)
	if err != nil {
		c.boxErr = "SETTINGS_ENCRYPTION_KEY 格式不正確（需要 64 個 hex 字元，或 32 bytes 的 base64，可用 openssl rand -base64 32 產生）"
		log.Printf("[google-calendar] SETTINGS_ENCRYPTION_KEY is invalid: %v", err)
		return c
	}
	c.box = box
	return c
}

func (c *GoogleCalendarController) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/settings/google-calendar", c.handleStatus)
	mux.HandleFunc("/api/settings/google-calendar/credentials", c.handleUploadCredentials)
	mux.HandleFunc("/api/settings/google-calendar/connect", c.handleConnect)
	mux.HandleFunc(domain.GoogleCalendarCallbackPath, c.handleCallback)
	mux.HandleFunc("/api/settings/google-calendar/test", c.handleTest)
	mux.HandleFunc("/api/settings/google-calendar/calendars", c.handleSelectCalendars)
	mux.HandleFunc("/api/settings/google-calendar/events", c.handleEventsPreview)
	mux.HandleFunc("/api/settings/google-calendar/disconnect", c.handleDisconnect)
}

type googleCalendarStatus struct {
	EncryptionReady bool   `json:"encryption_ready"`
	EncryptionError string `json:"encryption_error,omitempty"`
	ClientID        string `json:"client_id"`
	HasClientSecret bool   `json:"has_client_secret"`
	// RedirectURIs are the callback URLs registered for the uploaded client;
	// CallbackPath is what the admin must have registered (origin + this).
	RedirectURIs []string `json:"redirect_uris"`
	CallbackPath string   `json:"callback_path"`
	// Connected means a Google account has been authorised (a refresh token is stored).
	Connected           bool     `json:"connected"`
	ConnectedAccount    string   `json:"connected_account,omitempty"`
	SelectedCalendarIDs []string `json:"selected_calendar_ids"`
	UpdatedAt           string   `json:"updated_at,omitempty"`
}

func (c *GoogleCalendarController) status(s *domain.GoogleCalendarSettings) googleCalendarStatus {
	out := googleCalendarStatus{
		EncryptionReady:     c.box != nil,
		EncryptionError:     c.boxErr,
		ClientID:            s.ClientID,
		HasClientSecret:     s.ClientSecretEnc != "",
		RedirectURIs:        s.RedirectURIs,
		CallbackPath:        domain.GoogleCalendarCallbackPath,
		Connected:           s.RefreshTokenEnc != "",
		ConnectedAccount:    s.ConnectedAccount,
		SelectedCalendarIDs: s.SelectedCalendarIDs,
	}
	if out.RedirectURIs == nil {
		out.RedirectURIs = []string{}
	}
	if out.SelectedCalendarIDs == nil {
		out.SelectedCalendarIDs = []string{}
	}
	if !s.UpdatedAt.IsZero() {
		out.UpdatedAt = s.UpdatedAt.Format(time.RFC3339)
	}
	return out
}

// clientSecret decrypts the stored client secret. Errors if nothing is stored
// or the key no longer matches the one it was encrypted with.
func (c *GoogleCalendarController) clientSecret(s *domain.GoogleCalendarSettings) (string, error) {
	if c.box == nil {
		return "", errors.New(c.boxErr)
	}
	if s.ClientSecretEnc == "" {
		return "", errors.New("尚未上傳 Google OAuth 憑證")
	}
	return c.box.Decrypt(s.ClientSecretEnc, purposeGoogleClientSecret)
}

func (c *GoogleCalendarController) refreshToken(s *domain.GoogleCalendarSettings) (string, error) {
	if c.box == nil {
		return "", errors.New(c.boxErr)
	}
	if s.RefreshTokenEnc == "" {
		return "", errors.New("尚未連結 Google 帳號")
	}
	return c.box.Decrypt(s.RefreshTokenEnc, purposeGoogleRefreshToken)
}

// revokeBestEffort tells Google to drop the refresh token when we forget it,
// so deleting our copy doesn't leave a still-valid token out there. Failure
// is logged (without the token) and otherwise ignored: the local removal is
// what matters, and an already-revoked token fails here harmlessly.
func (c *GoogleCalendarController) revokeBestEffort(ctx context.Context, s *domain.GoogleCalendarSettings) {
	token, err := c.refreshToken(s)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.google.Revoke(ctx, token); err != nil {
		log.Printf("[google-calendar] could not revoke token at Google (ignored): %v", err)
	}
}

// --- state: ties the callback to the admin who started the flow ---

type oauthState struct {
	UID string `json:"u"`
	Exp int64  `json:"e"`
	RU  string `json:"r"`
}

// makeOAuthState seals who started the flow, until when, and the exact
// redirect URI used, with the same AEAD as the stored secrets, so it can be
// neither read nor forged by anyone without the key.
func makeOAuthState(box *secretbox.Box, uid, redirectURI string, now time.Time) (string, error) {
	b, _ := json.Marshal(oauthState{UID: uid, Exp: now.Add(oauthStateTTL).Unix(), RU: redirectURI})
	return box.Encrypt(string(b), purposeGoogleOAuthState)
}

var errBadState = errors.New("授權連結已失效或不正確，請回到設定頁重新按「連結 Google 帳號」")

// parseOAuthState returns the redirect URI used when the flow started, if the
// state is genuine, unexpired and was issued to this same signed-in admin
// (so a callback link crafted for someone else's session is useless).
func parseOAuthState(box *secretbox.Box, state, uid string, now time.Time) (string, error) {
	plain, err := box.Decrypt(state, purposeGoogleOAuthState)
	if err != nil {
		return "", errBadState
	}
	var st oauthState
	if json.Unmarshal([]byte(plain), &st) != nil || st.UID == "" || st.UID != uid || now.Unix() > st.Exp || st.RU == "" {
		return "", errBadState
	}
	return st.RU, nil
}

// --- handlers ---

// handleStatus godoc
// @Summary 取得 / 清除 Google 日曆連線狀態（sys_admin；絕不回傳密鑰）
// @Tags Settings
// @Produce json
// @Security BearerAuth
// @Router /api/settings/google-calendar [get]
// @Router /api/settings/google-calendar [delete]
func (c *GoogleCalendarController) handleStatus(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireSysAdmin(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		s, err := c.repo.Get(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, c.status(s))

	case http.MethodDelete:
		if s, err := c.repo.Get(r.Context()); err == nil {
			c.revokeBestEffort(r.Context(), s)
		}
		if err := c.repo.Clear(r.Context(), claims.UserID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		log.Printf("[google-calendar] credentials removed by user=%s", claims.UserID)
		writeOK(w)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleUploadCredentials godoc
// @Summary 上傳 Google OAuth 用戶端 JSON（加密後存入資料庫，sys_admin）
// @Description 接受 multipart（欄位名 file）或直接以 application/json 傳 JSON 本文。上傳新憑證會清除既有的授權（refresh token），需重新授權。
// @Tags Settings
// @Accept multipart/form-data
// @Accept json
// @Produce json
// @Security BearerAuth
// @Router /api/settings/google-calendar/credentials [post]
func (c *GoogleCalendarController) handleUploadCredentials(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireSysAdmin(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	// Fail before reading anything if there's no way to store it safely.
	if c.box == nil {
		writeError(w, http.StatusServiceUnavailable, c.boxErr)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxCredentialsFileBytes)
	var data []byte
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(maxCredentialsFileBytes); err != nil {
			writeError(w, http.StatusBadRequest, "檔案太大或表單格式錯誤")
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "請上傳檔案（欄位名稱 file）")
			return
		}
		defer file.Close()
		if data, err = io.ReadAll(file); err != nil {
			writeError(w, http.StatusBadRequest, "讀取檔案失敗")
			return
		}
	} else {
		var err error
		if data, err = io.ReadAll(r.Body); err != nil {
			writeError(w, http.StatusBadRequest, "內容太大或讀取失敗")
			return
		}
	}

	client, err := domain.ParseGoogleOAuthClientJSON(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	enc, err := c.box.Encrypt(client.ClientSecret, purposeGoogleClientSecret)
	if err != nil {
		log.Printf("[google-calendar] encrypt failed: %v", err)
		writeError(w, http.StatusInternalServerError, "加密失敗")
		return
	}
	// Replacing the client makes the old refresh token useless, so also tell
	// Google to drop it rather than leave it valid but unreachable.
	if old, err := c.repo.Get(r.Context()); err == nil {
		c.revokeBestEffort(r.Context(), old)
	}
	if err := c.repo.SaveClient(r.Context(), client.ClientID, enc, client.RedirectURIs, claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Log who and which client — never the secret.
	log.Printf("[google-calendar] credentials uploaded by user=%s client_id=%s", claims.UserID, client.ClientID)

	s, err := c.repo.Get(r.Context())
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": true})
		return
	}
	writeJSON(w, c.status(s))
}

// handleConnect godoc
// @Summary 開始 Google 授權：回傳要導向的 Google 同意畫面網址（sys_admin）
// @Description body 帶 origin（目前頁面的網址來源，例如 https://mdm.isha.net）。回呼網址 = origin + /api/settings/google-calendar/callback，必須是憑證裡已註冊的 URI。
// @Tags Settings
// @Accept json
// @Produce json
// @Security BearerAuth
// @Router /api/settings/google-calendar/connect [post]
func (c *GoogleCalendarController) handleConnect(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireSysAdmin(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if c.box == nil {
		writeError(w, http.StatusServiceUnavailable, c.boxErr)
		return
	}

	var body struct {
		Origin string `json:"origin"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	s, err := c.repo.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.ClientID == "" || s.ClientSecretEnc == "" {
		writeError(w, http.StatusBadRequest, "請先上傳 Google OAuth 憑證 JSON")
		return
	}
	redirectURI, err := domain.PickGoogleRedirectURI(s.RedirectURIs, body.Origin)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	state, err := makeOAuthState(c.box, claims.UserID, redirectURI, c.now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "無法建立授權狀態")
		return
	}
	writeJSON(w, map[string]string{"url": c.google.AuthCodeURL(s.ClientID, redirectURI, state), "redirect_uri": redirectURI})
}

// settingsRedirect sends the admin's browser back to the settings page with
// the outcome in the query string (the callback is a browser navigation from
// Google, not an API call, so it can't answer with JSON).
func settingsRedirect(w http.ResponseWriter, r *http.Request, ok bool, msg string) {
	q := url.Values{}
	if ok {
		q.Set("google_calendar", "connected")
	} else {
		q.Set("google_calendar", "error")
		if len(msg) > 300 {
			msg = msg[:300]
		}
		q.Set("msg", msg)
	}
	http.Redirect(w, r, "/admin/settings?"+q.Encode(), http.StatusSeeOther)
}

// googleErrText turns an error from Google into something the admin can act
// on, without exposing anything but Google's own short error code.
func googleErrText(err error) string {
	var ae *google.APIError
	switch {
	case google.IsInvalidGrant(err):
		return "Google 授權已失效（可能已被撤銷或過期），請重新連結 Google 帳號"
	case errors.As(err, &ae) && ae.Code == "redirect_uri_mismatch":
		return "Google 回報 redirect_uri_mismatch：請確認憑證裡已註冊目前網址的回呼 URI"
	case errors.As(err, &ae) && ae.Code == "invalid_client":
		return "Google 回報 invalid_client：用戶端 ID 或密鑰不正確，請重新上傳憑證 JSON"
	case errors.As(err, &ae) && ae.Code == "PERMISSION_DENIED":
		return "Google 拒絕存取日曆：請確認專案已啟用 Google Calendar API（" + ae.Description + "）"
	default:
		return err.Error()
	}
}

// handleCallback godoc
// @Summary Google 授權完成後的回呼（瀏覽器導向，不是給程式呼叫的 API）
// @Tags Settings
// @Router /api/settings/google-calendar/callback [get]
func (c *GoogleCalendarController) handleCallback(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireSysAdmin(r)
	if err != nil {
		settingsRedirect(w, r, false, "登入已逾時或權限不足，請重新登入後再試一次")
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if c.box == nil {
		settingsRedirect(w, r, false, c.boxErr)
		return
	}
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		if e == "access_denied" {
			settingsRedirect(w, r, false, "您取消了 Google 授權")
		} else {
			settingsRedirect(w, r, false, "Google 回報錯誤："+e)
		}
		return
	}
	redirectURI, err := parseOAuthState(c.box, q.Get("state"), claims.UserID, c.now())
	if err != nil {
		settingsRedirect(w, r, false, err.Error())
		return
	}
	code := q.Get("code")
	if code == "" {
		settingsRedirect(w, r, false, "Google 沒有回傳授權碼，請重試")
		return
	}

	s, err := c.repo.Get(r.Context())
	if err != nil {
		settingsRedirect(w, r, false, "讀取設定失敗")
		return
	}
	secret, err := c.clientSecret(s)
	if err != nil {
		log.Printf("[google-calendar] cannot read client secret: %v", err)
		settingsRedirect(w, r, false, "無法讀取已儲存的用戶端密鑰，請重新上傳憑證 JSON（SETTINGS_ENCRYPTION_KEY 可能已更換）")
		return
	}
	tok, err := c.google.ExchangeCode(r.Context(), s.ClientID, secret, code, redirectURI)
	if err != nil {
		log.Printf("[google-calendar] code exchange failed: %v", err)
		settingsRedirect(w, r, false, googleErrText(err))
		return
	}
	if tok.RefreshToken == "" {
		// Google only omits it when it believes this client already holds one
		// that was not revoked; removing the app's access makes it issue a new one.
		settingsRedirect(w, r, false, "Google 沒有提供長期授權（refresh token）。請到 https://myaccount.google.com/permissions 移除此應用程式的存取權後重新連結")
		return
	}
	// Prove the account can actually read calendars (API enabled, scope
	// granted) before calling it "connected", and learn which account it is.
	cals, err := c.google.ListCalendars(r.Context(), tok.AccessToken)
	if err != nil {
		log.Printf("[google-calendar] listing calendars after authorisation failed: %v", err)
		settingsRedirect(w, r, false, "已取得授權但無法讀取日曆："+googleErrText(err))
		return
	}
	account := ""
	for _, cal := range cals {
		if cal.Primary {
			account = cal.ID
			break
		}
	}
	enc, err := c.box.Encrypt(tok.RefreshToken, purposeGoogleRefreshToken)
	if err != nil {
		settingsRedirect(w, r, false, "加密失敗")
		return
	}
	// A previous connection's token is replaced, so don't leave it valid.
	c.revokeBestEffort(r.Context(), s)
	if err := c.repo.SaveRefreshToken(r.Context(), enc, account, claims.UserID); err != nil {
		settingsRedirect(w, r, false, "儲存授權失敗")
		return
	}
	log.Printf("[google-calendar] account connected by user=%s account=%s", claims.UserID, account)
	settingsRedirect(w, r, true, "")
}

// handleTest godoc
// @Summary 測試連線：用已儲存的授權列出此 Google 帳號看得到（含訂閱）的日曆（sys_admin）
// @Tags Settings
// @Produce json
// @Security BearerAuth
// @Router /api/settings/google-calendar/test [post]
func (c *GoogleCalendarController) handleTest(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireSysAdmin(r); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	s, err := c.repo.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	secret, err := c.clientSecret(s)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	refresh, err := c.refreshToken(s)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	access, err := c.google.RefreshAccessToken(r.Context(), s.ClientID, secret, refresh)
	if err != nil {
		log.Printf("[google-calendar] test: token refresh failed: %v", err)
		status := http.StatusBadGateway
		if google.IsInvalidGrant(err) {
			status = http.StatusConflict
		}
		writeError(w, status, googleErrText(err))
		return
	}
	cals, err := c.google.ListCalendars(r.Context(), access)
	if err != nil {
		log.Printf("[google-calendar] test: listing calendars failed: %v", err)
		writeError(w, http.StatusBadGateway, googleErrText(err))
		return
	}
	if cals == nil {
		cals = []google.Calendar{}
	}
	writeJSON(w, map[string]interface{}{"account": s.ConnectedAccount, "calendars": cals})
}

// handleDisconnect godoc
// @Summary 中斷 Google 帳號連結（保留已上傳的憑證，sys_admin）
// @Tags Settings
// @Produce json
// @Security BearerAuth
// @Router /api/settings/google-calendar/disconnect [post]
func (c *GoogleCalendarController) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireSysAdmin(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	if s, err := c.repo.Get(r.Context()); err == nil {
		c.revokeBestEffort(r.Context(), s)
	}
	if err := c.repo.ClearRefreshToken(r.Context(), claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Printf("[google-calendar] account disconnected by user=%s", claims.UserID)
	s, err := c.repo.Get(r.Context())
	if err != nil {
		writeOK(w)
		return
	}
	writeJSON(w, c.status(s))
}

// --- choosing calendars and previewing their events ---

const (
	maxSelectedCalendars = 20
	maxCalendarIDLen     = 300
	defaultPreviewDays   = 60
	maxPreviewDays       = 366
	maxDescriptionChars  = 4000
)

// cleanCalendarIDs trims, drops blanks and duplicates (keeping first-seen
// order), and enforces sane limits so the setting can't be filled with junk.
func cleanCalendarIDs(in []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if len(id) > maxCalendarIDLen {
			return nil, errors.New("日曆 ID 太長")
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) > maxSelectedCalendars {
		return nil, fmt.Errorf("最多只能選 %d 個日曆", maxSelectedCalendars)
	}
	return out, nil
}

// handleSelectCalendars godoc
// @Summary 設定要讀取哪些日曆（sys_admin）
// @Tags Settings
// @Accept json
// @Produce json
// @Security BearerAuth
// @Router /api/settings/google-calendar/calendars [put]
func (c *GoogleCalendarController) handleSelectCalendars(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireSysAdmin(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPut) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var body struct {
		CalendarIDs []string `json:"calendar_ids"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	ids, err := cleanCalendarIDs(body.CalendarIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s, err := c.repo.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.RefreshTokenEnc == "" {
		writeError(w, http.StatusBadRequest, "尚未連結 Google 帳號")
		return
	}
	if err := c.repo.SaveSelectedCalendars(r.Context(), ids, claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.SelectedCalendarIDs = ids
	writeJSON(w, c.status(s))
}

type previewEvent struct {
	google.Event
	CalendarID      string       `json:"calendar_id"`
	CalendarSummary string       `json:"calendar_summary"`
	Trip            tripcar.Trip `json:"trip"`
	// LocationUsed is what the destination check looked at: the "地點："line in
	// the description if there is one, otherwise the event's own location field.
	LocationUsed   string `json:"location_used"`
	LocationSource string `json:"location_source"` // "description", "event" or ""
	TargetLocation bool   `json:"target_location"`
	Truncated      bool   `json:"description_truncated,omitempty"`
}

type previewCalendar struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
	Count   int    `json:"count"`
	Error   string `json:"error,omitempty"`
}

func buildPreviewEvent(cal google.Calendar, ev google.Event) previewEvent {
	trip := tripcar.ParseDescription(ev.Description)
	if trip.Names == nil {
		trip.Names = []string{}
	}
	if trip.Emails == nil {
		trip.Emails = []string{}
	}
	p := previewEvent{Event: ev, CalendarID: cal.ID, CalendarSummary: cal.Summary, Trip: trip}
	switch {
	case trip.Location != "":
		p.LocationUsed, p.LocationSource = trip.Location, "description"
	case strings.TrimSpace(ev.Location) != "":
		p.LocationUsed, p.LocationSource = strings.TrimSpace(ev.Location), "event"
	}
	p.TargetLocation = tripcar.LocationMatches(p.LocationUsed, tripcar.DefaultTargetCounties)
	if r := []rune(p.Description); len(r) > maxDescriptionChars {
		p.Description, p.Truncated = string(r[:maxDescriptionChars]), true
	}
	return p
}

// startTime orders events for display. All-day events carry only a date; it
// is read as UTC midnight, which is only used to put days in order.
func startTime(ev google.Event) time.Time {
	if t, err := time.Parse(time.RFC3339, ev.Start); err == nil {
		return t
	}
	t, _ := time.Parse("2006-01-02", ev.Start)
	return t
}

// handleEventsPreview godoc
// @Summary 預覽已選日曆的近期活動，並顯示說明欄的解析結果（sys_admin，唯讀）
// @Description 用來驗證實際活動說明欄的格式與解析規則對不對得上；不會寄信、不會寫入任何東西。
// @Tags Settings
// @Produce json
// @Security BearerAuth
// @Param days query int false "往後看幾天（預設 60，最多 366）"
// @Router /api/settings/google-calendar/events [get]
func (c *GoogleCalendarController) handleEventsPreview(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireSysAdmin(r); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	days := defaultPreviewDays
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPreviewDays {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("days 需介於 1 到 %d", maxPreviewDays))
			return
		}
		days = n
	}

	s, err := c.repo.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(s.SelectedCalendarIDs) == 0 {
		writeError(w, http.StatusBadRequest, "請先選擇要讀取的日曆")
		return
	}
	secret, err := c.clientSecret(s)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	refresh, err := c.refreshToken(s)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	access, err := c.google.RefreshAccessToken(r.Context(), s.ClientID, secret, refresh)
	if err != nil {
		log.Printf("[google-calendar] preview: token refresh failed: %v", err)
		status := http.StatusBadGateway
		if google.IsInvalidGrant(err) {
			status = http.StatusConflict
		}
		writeError(w, status, googleErrText(err))
		return
	}
	// Names come from the calendar list; a selected id that's no longer on it
	// still gets queried (and will report its own error).
	names := map[string]google.Calendar{}
	if all, err := c.google.ListCalendars(r.Context(), access); err == nil {
		for _, cal := range all {
			names[cal.ID] = cal
		}
	}

	now := c.now()
	from, to := now.Add(-24*time.Hour), now.AddDate(0, 0, days) // yesterday on, so a trip already under way still shows
	cals := make([]previewCalendar, 0, len(s.SelectedCalendarIDs))
	events := []previewEvent{}
	for _, id := range s.SelectedCalendarIDs {
		cal, ok := names[id]
		if !ok {
			cal = google.Calendar{ID: id, Summary: id}
		}
		evs, err := c.google.ListEvents(r.Context(), access, id, from, to)
		pc := previewCalendar{ID: id, Summary: cal.Summary, Count: len(evs)}
		if err != nil {
			log.Printf("[google-calendar] preview: reading calendar failed: %v", err)
			pc.Error = googleErrText(err)
		}
		cals = append(cals, pc)
		for _, ev := range evs {
			events = append(events, buildPreviewEvent(cal, ev))
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return startTime(events[i].Event).Before(startTime(events[j].Event)) })

	writeJSON(w, map[string]interface{}{
		"days":      days,
		"targets":   tripcar.DefaultTargetCounties,
		"calendars": cals,
		"events":    events,
	})
}
