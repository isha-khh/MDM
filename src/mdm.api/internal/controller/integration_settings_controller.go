package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/anthropics/mdm-server/internal/adapter/postgres"
	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/port"
)

// IntegrationSettingsController lets a system administrator manage the
// integration API from the web UI: the API key, who may use it, and which
// asset each outward resource id stands for.
type IntegrationSettingsController struct {
	repo       *postgres.IntegrationRepo
	auditRepo  port.AuditRepository
	auth       *middleware.AuthHelper
	listenAddr string // integration listener address; empty = the API is switched off
}

func NewIntegrationSettingsController(repo *postgres.IntegrationRepo, auditRepo port.AuditRepository, auth *middleware.AuthHelper, listenAddr string) *IntegrationSettingsController {
	return &IntegrationSettingsController{repo: repo, auditRepo: auditRepo, auth: auth, listenAddr: listenAddr}
}

func (c *IntegrationSettingsController) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/integration", c.handleGet)
	mux.HandleFunc("PUT /api/settings/integration", c.handleUpdate)
	mux.HandleFunc("POST /api/settings/integration/key", c.handleKey)
	mux.HandleFunc("PUT /api/settings/integration/resources/{id}", c.handlePutResource)
	mux.HandleFunc("DELETE /api/settings/integration/resources/{id}", c.handleDeleteResource)
}

type integrationClientDTO struct {
	Name                   string   `json:"name"`
	KeyPrefix              string   `json:"key_prefix"`
	Enabled                bool     `json:"enabled"`
	IPAllowlist            []string `json:"ip_allowlist"`
	AutoCreateEmailDomains []string `json:"auto_create_email_domains"`
	CreatedAt              string   `json:"created_at"`
	RotatedAt              *string  `json:"rotated_at"`
	LastUsedAt             *string  `json:"last_used_at"`
}

type integrationResourceDTO struct {
	ResourceID    string   `json:"resource_id"`
	ServiceType   string   `json:"service_type"`
	AssetID       string   `json:"asset_id"`
	AssetNumber   string   `json:"asset_number"`
	AssetName     string   `json:"asset_name"`
	AllowedCities []string `json:"allowed_cities"`
	Enabled       bool     `json:"enabled"`
}

func timeStr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

func toClientDTO(c *domain.IntegrationClient) *integrationClientDTO {
	if c == nil {
		return nil
	}
	return &integrationClientDTO{
		Name: c.Name, KeyPrefix: c.KeyPrefix, Enabled: c.Enabled,
		IPAllowlist: nonNil(c.IPAllowlist), AutoCreateEmailDomains: nonNil(c.AutoCreateEmailDomains),
		CreatedAt: c.CreatedAt.Format(time.RFC3339), RotatedAt: timeStr(c.RotatedAt), LastUsedAt: timeStr(c.LastUsedAt),
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (c *IntegrationSettingsController) state(r *http.Request) (map[string]interface{}, error) {
	client, err := c.repo.PrimaryClient(r.Context())
	if err != nil {
		return nil, err
	}
	resources, err := c.repo.ListResources(r.Context())
	if err != nil {
		return nil, err
	}
	rs := make([]integrationResourceDTO, 0, len(resources))
	for _, x := range resources {
		rs = append(rs, integrationResourceDTO{
			ResourceID: x.ResourceID, ServiceType: x.ServiceType, AssetID: x.AssetID,
			AssetNumber: x.AssetNumber, AssetName: x.AssetName, AllowedCities: nonNil(x.AllowedCities), Enabled: x.Enabled,
		})
	}
	return map[string]interface{}{
		"listener_enabled": c.listenAddr != "",
		"listen_addr":      c.listenAddr,
		"client":           toClientDTO(client),
		"resources":        rs,
	}, nil
}

func (c *IntegrationSettingsController) audit(r *http.Request, claims *middleware.Claims, action, target, detail string) {
	c.auditRepo.Create(r.Context(), &domain.AuditLog{
		UserID: claims.UserID, Username: claims.Username, Action: action, Target: target,
		Detail: detail, Module: "system", IPAddress: clientIP(r), UserAgent: r.UserAgent(),
	})
}

// @Summary 取得整合 API 設定（sys_admin）
// @Tags Settings
// @Security BearerAuth
// @Router /api/settings/integration [get]
func (c *IntegrationSettingsController) handleGet(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireSysAdmin(r); err != nil {
		writeError(w, http.StatusForbidden, "sys_admin required")
		return
	}
	st, err := c.state(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, st)
}

// @Summary 發行或輪替整合 API key（sys_admin；明文只在這次回應出現）
// @Tags Settings
// @Security BearerAuth
// @Router /api/settings/integration/key [post]
func (c *IntegrationSettingsController) handleKey(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireSysAdmin(r)
	if err != nil {
		writeError(w, http.StatusForbidden, "sys_admin required")
		return
	}
	key, hash, prefix, err := middleware.GenerateKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot generate key")
		return
	}
	client, err := c.repo.SetKey(r.Context(), "派發系統", hash, prefix)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	c.audit(r, claims, "integration_key_issue", client.Name, "prefix="+prefix)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]interface{}{"key": key, "client": toClientDTO(client)})
}

// @Summary 更新整合 API 設定：啟用、IP 白名單、可自動建立帳號的信箱網域（sys_admin）
// @Tags Settings
// @Security BearerAuth
// @Router /api/settings/integration [put]
func (c *IntegrationSettingsController) handleUpdate(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireSysAdmin(r)
	if err != nil {
		writeError(w, http.StatusForbidden, "sys_admin required")
		return
	}
	var body struct {
		Enabled                *bool     `json:"enabled"`
		IPAllowlist            *[]string `json:"ip_allowlist"`
		AutoCreateEmailDomains *[]string `json:"auto_create_email_domains"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	client, err := c.repo.PrimaryClient(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if client == nil {
		writeError(w, http.StatusConflict, "尚未發行 API key")
		return
	}
	u := postgres.ClientUpdate{Enabled: body.Enabled}
	if body.IPAllowlist != nil {
		list := make([]string, 0, len(*body.IPAllowlist))
		for _, e := range *body.IPAllowlist {
			e = strings.TrimSpace(e)
			if e == "" {
				continue
			}
			if err := domain.ValidateIPEntry(e); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			list = append(list, e)
		}
		u.IPAllowlist = &list
	}
	if body.AutoCreateEmailDomains != nil {
		list := make([]string, 0, len(*body.AutoCreateEmailDomains))
		for _, d := range *body.AutoCreateEmailDomains {
			d = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(d), "@")))
			if d == "" {
				continue
			}
			if !domainPattern.MatchString(d) {
				writeError(w, http.StatusBadRequest, "「"+d+"」不是有效的網域")
				return
			}
			list = append(list, d)
		}
		u.AutoCreateEmailDomains = &list
	}
	updated, err := c.repo.UpdateClient(r.Context(), client.ID, u)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	detail, _ := json.Marshal(map[string]interface{}{
		"enabled": updated.Enabled, "ip_allowlist": updated.IPAllowlist, "auto_create_email_domains": updated.AutoCreateEmailDomains,
	})
	c.audit(r, claims, "integration_settings_update", updated.Name, string(detail))
	st, err := c.state(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, st)
}

var (
	domainPattern     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	resourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	serviceTypes      = map[string]bool{"RENTAL_CAR": true}
)

// @Summary 設定資源代號（如 RENTAL_CAR_01）對應的資產（sys_admin）
// @Tags Settings
// @Security BearerAuth
// @Router /api/settings/integration/resources/{id} [put]
func (c *IntegrationSettingsController) handlePutResource(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireSysAdmin(r)
	if err != nil {
		writeError(w, http.StatusForbidden, "sys_admin required")
		return
	}
	id := r.PathValue("id")
	if !resourceIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "resource id 只能用英數、底線與連字號（64 字內）")
		return
	}
	var body struct {
		ServiceType   string   `json:"service_type"`
		AssetID       string   `json:"asset_id"`
		AllowedCities []string `json:"allowed_cities"`
		Enabled       *bool    `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AssetID == "" {
		writeError(w, http.StatusBadRequest, "asset_id required")
		return
	}
	if body.ServiceType == "" {
		body.ServiceType = "RENTAL_CAR"
	}
	if !serviceTypes[body.ServiceType] {
		writeError(w, http.StatusBadRequest, "service_type 目前僅支援 RENTAL_CAR")
		return
	}
	cities := make([]string, 0, len(body.AllowedCities))
	for _, city := range body.AllowedCities {
		if city = strings.TrimSpace(city); city != "" {
			cities = append(cities, city)
		}
	}
	enabled := body.Enabled == nil || *body.Enabled
	res := &domain.IntegrationResource{ResourceID: id, ServiceType: body.ServiceType, AssetID: body.AssetID, AllowedCities: cities, Enabled: enabled}
	if err := c.repo.UpsertResource(r.Context(), res); err != nil {
		if errors.Is(err, postgres.ErrAssetNotFound) {
			writeError(w, http.StatusBadRequest, "找不到這個資產")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	c.audit(r, claims, "integration_resource_set", id, "asset_id="+body.AssetID)
	st, err := c.state(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, st)
}

// @Summary 移除資源對應（sys_admin）；已建立的租借不受影響
// @Tags Settings
// @Security BearerAuth
// @Router /api/settings/integration/resources/{id} [delete]
func (c *IntegrationSettingsController) handleDeleteResource(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireSysAdmin(r)
	if err != nil {
		writeError(w, http.StatusForbidden, "sys_admin required")
		return
	}
	id := r.PathValue("id")
	ok, err := c.repo.DeleteResource(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "resource not found")
		return
	}
	c.audit(r, claims, "integration_resource_delete", id, "")
	st, err := c.state(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, st)
}
