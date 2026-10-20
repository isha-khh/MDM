package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/mdm-server/internal/adapter/directory"
	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/port"
	"github.com/anthropics/mdm-server/internal/secretbox"
	"github.com/anthropics/mdm-server/internal/service/tripcar"
)

const purposeDirectoryAPIKey = "directory.api_key"

// maxAPIKeyLen only stops junk being stored; real keys are far shorter.
const maxAPIKeyLen = 512

// ErrDirectoryNotConfigured means no endpoint / API key has been saved yet.
var ErrDirectoryNotConfigured = errors.New("尚未設定人員目錄的網址與 API key")

type directoryAPI interface {
	ListUsers(ctx context.Context, baseURL, apiKey string) ([]directory.User, error)
}

// DirectoryController manages the connection to the company user directory
// (who has a driver's license / a car of their own) and serves it to the trip
// preview. Sys-admin only; the API key is encrypted at rest and never returned.
type DirectoryController struct {
	repo   port.DirectorySettingsRepository
	client directoryAPI
	box    *secretbox.Box
	boxErr string
	auth   *middleware.AuthHelper
}

func NewDirectoryController(repo port.DirectorySettingsRepository, encryptionKey string, auth *middleware.AuthHelper, client directoryAPI) *DirectoryController {
	c := &DirectoryController{repo: repo, client: client, auth: auth}
	c.box, c.boxErr = newSettingsBox(encryptionKey, "directory")
	return c
}

func (c *DirectoryController) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/settings/directory", c.handleSettings)
	mux.HandleFunc("/api/settings/directory/test", c.handleTest)
}

// Users loads the directory with the stored connection, mapped to what the
// trip rules need.
func (c *DirectoryController) Users(ctx context.Context) ([]tripcar.User, error) {
	if c.box == nil {
		return nil, errors.New(c.boxErr)
	}
	s, err := c.repo.Get(ctx)
	if err != nil {
		return nil, err
	}
	if s.BaseURL == "" || s.APIKeyEnc == "" {
		return nil, ErrDirectoryNotConfigured
	}
	key, err := c.box.Decrypt(s.APIKeyEnc, purposeDirectoryAPIKey)
	if err != nil {
		return nil, errors.New("無法讀取已儲存的 API key，請重新輸入（SETTINGS_ENCRYPTION_KEY 可能已更換）")
	}
	raw, err := c.client.ListUsers(ctx, s.BaseURL, key)
	if err != nil {
		return nil, err
	}
	users := make([]tripcar.User, 0, len(raw))
	for _, u := range raw {
		users = append(users, tripcar.User{
			SamAccountName: u.SamAccountName, DisplayName: u.DisplayName, Mail: u.Mail,
			Disabled: !u.Enabled, HasDriverLicense: u.HasDriverLicense, HasOwnCar: u.HasOwnCar,
		})
	}
	return users, nil
}

type directoryStatus struct {
	EncryptionReady bool   `json:"encryption_ready"`
	EncryptionError string `json:"encryption_error,omitempty"`
	BaseURL         string `json:"base_url"`
	HasAPIKey       bool   `json:"has_api_key"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

func (c *DirectoryController) status(s *domain.DirectorySettings) directoryStatus {
	out := directoryStatus{EncryptionReady: c.box != nil, EncryptionError: c.boxErr, BaseURL: s.BaseURL, HasAPIKey: s.APIKeyEnc != ""}
	if !s.UpdatedAt.IsZero() {
		out.UpdatedAt = s.UpdatedAt.Format(time.RFC3339)
	}
	return out
}

// handleSettings godoc
// @Summary 人員目錄 API（駕照／有無自己的車）連線設定（sys_admin；API key 加密儲存、絕不回傳）
// @Tags Settings
// @Accept json
// @Produce json
// @Security BearerAuth
// @Router /api/settings/directory [get]
// @Router /api/settings/directory [put]
// @Router /api/settings/directory [delete]
func (c *DirectoryController) handleSettings(w http.ResponseWriter, r *http.Request) {
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

	case http.MethodPut:
		if c.box == nil {
			writeError(w, http.StatusServiceUnavailable, c.boxErr)
			return
		}
		var body struct {
			BaseURL string `json:"base_url"`
			// APIKey is write-only. Left empty, the stored key is kept, so the
			// endpoint can be changed without re-entering it.
			APIKey string `json:"api_key"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
		base, err := directory.NormalizeBaseURL(body.BaseURL)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		key := strings.TrimSpace(body.APIKey)
		if len(key) > maxAPIKeyLen {
			writeError(w, http.StatusBadRequest, "API key 太長")
			return
		}
		existing, err := c.repo.Get(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		enc := existing.APIKeyEnc
		if key != "" {
			if enc, err = c.box.Encrypt(key, purposeDirectoryAPIKey); err != nil {
				writeError(w, http.StatusInternalServerError, "加密失敗")
				return
			}
		}
		if enc == "" {
			writeError(w, http.StatusBadRequest, "請輸入 API key")
			return
		}
		if err := c.repo.Save(r.Context(), base, enc, claims.UserID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		log.Printf("[directory] settings saved by user=%s base_url=%s key_changed=%v", claims.UserID, base, key != "")
		s, err := c.repo.Get(r.Context())
		if err != nil {
			writeOK(w)
			return
		}
		writeJSON(w, c.status(s))

	case http.MethodDelete:
		if err := c.repo.Clear(r.Context(), claims.UserID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		log.Printf("[directory] settings removed by user=%s", claims.UserID)
		writeOK(w)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

type tri struct {
	Yes     int `json:"yes"`
	No      int `json:"no"`
	Unknown int `json:"unknown"`
}

func (t *tri) add(v *bool) {
	switch {
	case v == nil:
		t.Unknown++
	case *v:
		t.Yes++
	default:
		t.No++
	}
}

// handleTest godoc
// @Summary 測試人員目錄連線，回傳統計（不回傳任何人的資料）（sys_admin）
// @Tags Settings
// @Produce json
// @Security BearerAuth
// @Router /api/settings/directory/test [post]
func (c *DirectoryController) handleTest(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireSysAdmin(r); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	users, err := c.Users(r.Context())
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, ErrDirectoryNotConfigured) {
			status = http.StatusBadRequest
		}
		log.Printf("[directory] test failed: %v", err)
		writeError(w, status, err.Error())
		return
	}
	// "People" = enabled accounts with a mail address: the personal accounts
	// that can actually be matched and notified. Service accounts and old
	// short accounts don't have one.
	var total, enabled, people int
	var license, car tri
	for _, u := range users {
		total++
		if u.Disabled {
			continue
		}
		enabled++
		if u.Mail == "" {
			continue
		}
		people++
		license.add(u.HasDriverLicense)
		car.add(u.HasOwnCar)
	}
	writeJSON(w, map[string]interface{}{
		"total_accounts":   total,
		"enabled_accounts": enabled,
		"people":           people,
		"has_license":      license,
		"has_own_car":      car,
	})
}
