package controller

import (
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/port"
	"github.com/anthropics/mdm-server/internal/secretbox"
)

// The purpose binds the ciphertext to the column it belongs in (see secretbox).
const purposeGoogleClientSecret = "google_calendar.client_secret"

// maxCredentialsFileBytes: a Google OAuth client JSON is well under 1KB; the
// cap just stops someone uploading something huge to this endpoint.
const maxCredentialsFileBytes = 64 << 10

// GoogleCalendarController manages the stored Google Calendar connection:
// uploading the OAuth client credentials (encrypted at rest), showing their
// status, and removing them. Sys-admin only, like the mail settings.
type GoogleCalendarController struct {
	repo port.GoogleCalendarSettingsRepository
	box  *secretbox.Box // nil when SETTINGS_ENCRYPTION_KEY is unset/invalid
	// boxErr says why box is nil, shown to the admin so the fix is obvious.
	boxErr string
	auth   *middleware.AuthHelper
}

// NewGoogleCalendarController takes the raw SETTINGS_ENCRYPTION_KEY value. An
// empty or malformed key doesn't stop the server (this is an optional
// feature); the endpoints just refuse to store secrets and say why.
func NewGoogleCalendarController(repo port.GoogleCalendarSettingsRepository, encryptionKey string, auth *middleware.AuthHelper) *GoogleCalendarController {
	c := &GoogleCalendarController{repo: repo, auth: auth}
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
}

type googleCalendarStatus struct {
	EncryptionReady bool   `json:"encryption_ready"`
	EncryptionError string `json:"encryption_error,omitempty"`
	ClientID        string `json:"client_id"`
	HasClientSecret bool   `json:"has_client_secret"`
	// Connected means a Google account has been authorised (a refresh token is stored).
	Connected bool   `json:"connected"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func (c *GoogleCalendarController) status(s *domain.GoogleCalendarSettings) googleCalendarStatus {
	out := googleCalendarStatus{
		EncryptionReady: c.box != nil,
		EncryptionError: c.boxErr,
		ClientID:        s.ClientID,
		HasClientSecret: s.ClientSecretEnc != "",
		Connected:       s.RefreshTokenEnc != "",
	}
	if !s.UpdatedAt.IsZero() {
		out.UpdatedAt = s.UpdatedAt.Format(time.RFC3339)
	}
	return out
}

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
	if err := c.repo.SaveClient(r.Context(), client.ClientID, enc, claims.UserID); err != nil {
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
