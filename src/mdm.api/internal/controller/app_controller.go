package controller

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/mdm-server/internal/adapter/postgres"
	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/port"
)

type AppController struct {
	appRepo    *postgres.AppRepo
	deviceRepo *postgres.DeviceRepo
	mdmClient  port.MicroMDMClient
	vppClient  port.VPPClient
	auditRepo  port.AuditRepository
	auth       *middleware.AuthHelper
}

func NewAppController(appRepo *postgres.AppRepo, deviceRepo *postgres.DeviceRepo, mdmClient port.MicroMDMClient, vppClient port.VPPClient, auditRepo port.AuditRepository, auth *middleware.AuthHelper) *AppController {
	return &AppController{appRepo: appRepo, deviceRepo: deviceRepo, mdmClient: mdmClient, vppClient: vppClient, auditRepo: auditRepo, auth: auth}
}

func (c *AppController) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/managed-apps", c.handleManagedApps)
	mux.HandleFunc("/api/managed-apps/", c.handleManagedAppByID)
	mux.HandleFunc("/api/device-apps", c.handleDeviceApps)
	mux.HandleFunc("/api/device-apps/install", c.handleInstall)
	mux.HandleFunc("/api/device-apps/update", c.handleUpdate)
	mux.HandleFunc("/api/device-apps/uninstall", c.handleUninstall)
	mux.HandleFunc("/api/sync-device-apps", c.handleSyncDeviceApps)
	mux.HandleFunc("/api/itunes-lookup", c.handleItunesLookup)
	mux.HandleFunc("/api/itunes-search", c.handleItunesSearch)
	mux.HandleFunc("/api/managed-apps/sync-vpp", c.handleSyncVPPAssets)
}

// handleSyncVPPAssets godoc
// @Summary 從 Apple VPP 拉回授權數量並更新 managed_apps.purchased_qty
// @Tags App
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "{total_assets, updated, unmatched: [...]}"
// @Failure 502 {object} swagError "VPP API 失敗"
// @Router /api/managed-apps/sync-vpp [post]
func (c *AppController) handleSyncVPPAssets(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireModule(r, "mdm", "operator"); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if c.vppClient == nil {
		writeError(w, http.StatusFailedDependency, "VPP not configured")
		return
	}

	assets, err := c.vppClient.GetVPPAssets(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	updated := 0
	unmatched := []map[string]interface{}{}
	for _, a := range assets {
		n, err := c.appRepo.SetPurchasedQtyByItunesID(r.Context(), a.AdamID, a.TotalCount)
		if err != nil {
			log.Printf("[vpp-sync] update adam=%s: %v", a.AdamID, err)
			continue
		}
		if n == 0 {
			// VPP asset Apple shows us but we haven't catalogued — surface to UI
			// so the admin can decide whether to add it as a managed_app.
			unmatched = append(unmatched, map[string]interface{}{
				"adam_id":         a.AdamID,
				"total_count":     a.TotalCount,
				"assigned_count":  a.AssignedCount,
				"product_type":    a.ProductTypeName,
			})
		} else {
			updated += n
		}
	}

	// Audit
	username, _ := r.Context().Value(middleware.CtxUsername).(string)
	userID, _ := r.Context().Value(middleware.CtxUserID).(string)
	_ = c.auditRepo.Create(r.Context(), &domain.AuditLog{
		UserID: userID, Username: username,
		Action: "sync_vpp_assets",
		Detail: fmt.Sprintf("assets=%d updated=%d unmatched=%d", len(assets), updated, len(unmatched)),
		Module: "mdm",
	})

	writeJSON(w, map[string]interface{}{
		"total_assets": len(assets),
		"updated":      updated,
		"unmatched":    unmatched,
	})
	log.Printf("[vpp-sync] assets=%d updated=%d unmatched=%d", len(assets), updated, len(unmatched))
}

// handleManagedApps godoc
// @Summary 受管 App 列表 / 新增受管 App
// @Tags App
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body swagManagedAppReq false "新增 App（POST）"
// @Success 200 {object} map[string]interface{} "GET: {apps: [...]}, POST: {id: ...}"
// @Failure 409 {object} swagError "重複"
// @Router /api/managed-apps [get]
// @Router /api/managed-apps [post]
func (c *AppController) handleManagedApps(w http.ResponseWriter, r *http.Request) {
	minLevel := "viewer"
	if r.Method == http.MethodPost {
		minLevel = "operator"
	}
	if _, err := c.auth.RequireModule(r, "mdm", minLevel); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		apps, err := c.appRepo.ListManagedApps(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rows := make([]map[string]interface{}, 0, len(apps))
		for _, a := range apps {
			rows = append(rows, map[string]interface{}{
				"id": a.ID, "name": a.Name, "bundle_id": a.BundleID,
				"app_type": a.AppType, "itunes_store_id": a.ItunesStoreID,
				"manifest_url": a.ManifestURL, "purchased_qty": a.PurchasedQty,
				"notes": a.Notes, "created_at": a.CreatedAt.Format(time.RFC3339),
				"updated_at": a.UpdatedAt.Format(time.RFC3339),
				"installed_count": a.InstalledCount, "icon_url": a.IconURL,
				"supported_platforms": a.SupportedPlatforms,
			})
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"apps": rows})

	case http.MethodPost:
		var body struct {
			Name               string `json:"name"`
			BundleID           string `json:"bundle_id"`
			AppType            string `json:"app_type"`
			ItunesStoreID      string `json:"itunes_store_id"`
			ManifestURL        string `json:"manifest_url"`
			PurchasedQty       int    `json:"purchased_qty"`
			Notes              string `json:"notes"`
			IconURL            string `json:"icon_url"`
			SupportedPlatforms string `json:"supported_platforms"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if body.AppType == "" {
			body.AppType = "vpp"
		}
		app := &domain.ManagedApp{
			Name: body.Name, BundleID: body.BundleID, AppType: body.AppType,
			ItunesStoreID: body.ItunesStoreID, ManifestURL: body.ManifestURL,
			PurchasedQty: body.PurchasedQty, Notes: body.Notes, IconURL: body.IconURL,
			SupportedPlatforms: body.SupportedPlatforms,
		}
		id, err := c.appRepo.CreateManagedApp(r.Context(), app)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"id": id})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleManagedAppByID godoc
// @Summary 更新 / 刪除受管 App
// @Tags App
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "App ID"
// @Param body body map[string]interface{} false "更新欄位（PUT）"
// @Success 200 {object} swagOK
// @Router /api/managed-apps/{id} [put]
// @Router /api/managed-apps/{id} [delete]
func (c *AppController) handleManagedAppByID(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireModule(r, "mdm", "operator"); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/managed-apps/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodPut:
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := c.appRepo.UpdateManagedApp(r.Context(), id, body); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeOK(w)

	case http.MethodDelete:
		if err := c.appRepo.DeleteManagedApp(r.Context(), id); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeOK(w)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleDeviceApps godoc
// @Summary 查詢裝置已安裝 App
// @Tags App
// @Produce json
// @Security BearerAuth
// @Param device_udid query string true "裝置 UDID"
// @Success 200 {object} map[string]interface{} "{device_apps: [...]}"
// @Failure 400 {object} swagError
// @Router /api/device-apps [get]
func (c *AppController) handleDeviceApps(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireModule(r, "mdm", "viewer"); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	deviceUdid := r.URL.Query().Get("device_udid")
	if deviceUdid == "" {
		writeError(w, http.StatusBadRequest, "device_udid required")
		return
	}
	items, err := c.appRepo.ListDeviceApps(r.Context(), deviceUdid)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	rows := make([]map[string]interface{}, 0, len(items))
	for _, d := range items {
		rows = append(rows, map[string]interface{}{
			"id": d.ID, "device_udid": d.DeviceUdid, "app_id": d.AppID,
			"installed_at": d.InstalledAt.Format(time.RFC3339),
			"app_name": d.AppName, "bundle_id": d.BundleID, "app_type": d.AppType,
		})
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"device_apps": rows})
}

// handleInstall godoc
// @Summary 安裝 App 到裝置
// @Description 向 MDM 發送 InstallApplication 指令，VPP App 會先指派授權
// @Tags App
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body swagAppActionReq true "App ID + 裝置 UDID"
// @Success 200 {object} swagCommandResp
// @Failure 404 {object} swagError "App 不存在"
// @Failure 409 {object} swagError "已安裝或超過授權數量"
// @Router /api/device-apps/install [post]
func (c *AppController) handleInstall(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireModule(r, "mdm", "operator")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var body struct {
		AppID string `json:"app_id"`
		UDID  string `json:"udid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AppID == "" || body.UDID == "" {
		writeError(w, http.StatusBadRequest, "app_id and udid required")
		return
	}

	log.Printf("[install-app] request received: app_id=%s udid=%s user=%s", body.AppID, body.UDID, claims.Username)

	app, err := c.appRepo.GetManagedApp(r.Context(), body.AppID)
	if err != nil {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}

	count, _ := c.appRepo.InstalledCount(r.Context(), body.AppID)
	if app.PurchasedQty > 0 && count >= app.PurchasedQty {
		writeError(w, http.StatusConflict, fmt.Sprintf("已達採購上限 (%d/%d)", count, app.PurchasedQty))
		return
	}

	installed, _ := c.appRepo.IsInstalledOn(r.Context(), body.UDID, body.AppID)
	if installed {
		writeError(w, http.StatusConflict, "此 App 已安裝在該裝置上")
		return
	}

	payload, err := c.buildInstallPayload(r, body.UDID, app)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	result, cmdErr := c.mdmClient.SendCommand(r.Context(), payload)
	if cmdErr != nil {
		writeError(w, http.StatusInternalServerError, cmdErr.Error())
		return
	}
	_ = c.mdmClient.SendPush(r.Context(), body.UDID)

	c.appRepo.CreatePendingCommand(r.Context(), &domain.PendingAppCommand{
		CommandUUID: result.CommandUUID, Action: "install", DeviceUdid: body.UDID, AppID: body.AppID,
	})

	_ = c.auditRepo.Create(r.Context(), &domain.AuditLog{
		UserID: claims.UserID, Username: claims.Username,
		Action: "install_app", Target: body.UDID, Detail: app.Name + " (" + app.BundleID + ")",
		Module: "mdm", IPAddress: clientIP(r), UserAgent: r.UserAgent(),
	})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true, "command_uuid": result.CommandUUID, "raw_response": result.RawResponse,
	})
}

// handleUpdate godoc
// @Summary 更新裝置上的 App
// @Tags App
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body swagAppActionReq true "App ID + 裝置 UDID"
// @Success 200 {object} swagCommandResp
// @Failure 404 {object} swagError
// @Router /api/device-apps/update [post]
func (c *AppController) handleUpdate(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireModule(r, "mdm", "operator")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var body struct {
		AppID string `json:"app_id"`
		UDID  string `json:"udid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AppID == "" || body.UDID == "" {
		writeError(w, http.StatusBadRequest, "app_id and udid required")
		return
	}

	app, err := c.appRepo.GetManagedApp(r.Context(), body.AppID)
	if err != nil {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}

	log.Printf("[update-app] request received: app_id=%s udid=%s user=%s appType=%s", body.AppID, body.UDID, claims.Username, app.AppType)

	payload, err := c.buildInstallPayload(r, body.UDID, app)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	result, cmdErr := c.mdmClient.SendCommand(r.Context(), payload)
	if cmdErr != nil {
		writeError(w, http.StatusInternalServerError, cmdErr.Error())
		return
	}
	_ = c.mdmClient.SendPush(r.Context(), body.UDID)

	_ = c.auditRepo.Create(r.Context(), &domain.AuditLog{
		UserID: claims.UserID, Username: claims.Username,
		Action: "update_app", Target: body.UDID, Detail: app.Name + " (" + app.BundleID + ")",
		Module: "mdm", IPAddress: clientIP(r), UserAgent: r.UserAgent(),
	})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true, "command_uuid": result.CommandUUID, "raw_response": result.RawResponse,
	})
}

// handleUninstall godoc
// @Summary 移除裝置上的 App
// @Tags App
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body swagAppActionReq true "App ID + 裝置 UDID"
// @Success 200 {object} swagCommandResp
// @Failure 404 {object} swagError
// @Router /api/device-apps/uninstall [post]
func (c *AppController) handleUninstall(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireModule(r, "mdm", "operator")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var body struct {
		AppID string `json:"app_id"`
		UDID  string `json:"udid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AppID == "" || body.UDID == "" {
		writeError(w, http.StatusBadRequest, "app_id and udid required")
		return
	}

	app, err := c.appRepo.GetManagedApp(r.Context(), body.AppID)
	if err != nil {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}

	result, cmdErr := c.mdmClient.SendCommand(r.Context(), map[string]interface{}{
		"udid": body.UDID, "request_type": "RemoveApplication", "identifier": app.BundleID,
	})
	if cmdErr != nil {
		writeError(w, http.StatusInternalServerError, cmdErr.Error())
		return
	}
	_ = c.mdmClient.SendPush(r.Context(), body.UDID)

	c.appRepo.CreatePendingCommand(r.Context(), &domain.PendingAppCommand{
		CommandUUID: result.CommandUUID, Action: "uninstall", DeviceUdid: body.UDID, AppID: body.AppID,
	})

	_ = c.auditRepo.Create(r.Context(), &domain.AuditLog{
		UserID: claims.UserID, Username: claims.Username,
		Action: "remove_app", Target: body.UDID, Detail: app.Name + " (" + app.BundleID + ")",
		Module: "mdm", IPAddress: clientIP(r), UserAgent: r.UserAgent(),
	})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true, "command_uuid": result.CommandUUID, "raw_response": result.RawResponse,
	})
}

// handleSyncDeviceApps godoc
// @Summary 同步所有裝置已安裝 App 到資料庫
// @Tags App
// @Produce json
// @Security BearerAuth
// @Success 200 {object} swagSyncAppsResp
// @Router /api/sync-device-apps [post]
func (c *AppController) handleSyncDeviceApps(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireModule(r, "mdm", "operator")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	bundleMap, err := c.appRepo.ListBundleMap(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(bundleMap) == 0 {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "synced": 0, "message": "no managed apps"})
		return
	}

	devices, _, err := c.deviceRepo.List(r.Context(), "", 5000, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	synced := 0
	for _, dev := range devices {
		if dev.Details == nil {
			continue
		}
		rawB64, ok := dev.Details["installed_apps_raw"]
		if !ok {
			continue
		}
		rawStr, ok := rawB64.(string)
		if !ok {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(rawStr)
		if err != nil {
			continue
		}
		xmlStr := string(decoded)
		remaining := xmlStr
		for {
			keyTag := "<key>Identifier</key>"
			pos := strings.Index(remaining, keyTag)
			if pos < 0 {
				break
			}
			after := remaining[pos+len(keyTag):]
			sStart := strings.Index(after, "<string>")
			sEnd := strings.Index(after, "</string>")
			if sStart < 0 || sEnd <= sStart {
				remaining = after
				continue
			}
			bundleID := after[sStart+8 : sEnd]
			remaining = after[sEnd:]
			if appID, ok := bundleMap[bundleID]; ok {
				created, _ := c.appRepo.SyncDeviceApp(r.Context(), dev.UDID, appID)
				if created {
					synced++
				}
			}
		}
	}

	_ = c.auditRepo.Create(r.Context(), &domain.AuditLog{
		UserID: claims.UserID, Username: claims.Username,
		Action: "sync_device_apps", Detail: fmt.Sprintf("synced %d bindings", synced),
		Module: "mdm", IPAddress: clientIP(r), UserAgent: r.UserAgent(),
	})
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "synced": synced})
}

// handleItunesLookup godoc
// @Summary iTunes Lookup（代理 Apple API）
// @Tags App
// @Produce json
// @Security BearerAuth
// @Param bundleId query string false "Bundle ID"
// @Param id query string false "iTunes Store ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/itunes-lookup [get]
func (c *AppController) handleItunesLookup(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireModule(r, "mdm", "viewer"); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	bundleID := r.URL.Query().Get("bundleId")
	itunesID := r.URL.Query().Get("id")
	if bundleID == "" && itunesID == "" {
		writeError(w, http.StatusBadRequest, "bundleId or id required")
		return
	}
	var lookupURL string
	if bundleID != "" {
		lookupURL = "https://itunes.apple.com/lookup?bundleId=" + bundleID + "&country=tw"
	} else {
		lookupURL = "https://itunes.apple.com/lookup?id=" + itunesID + "&country=tw"
	}
	resp, err := http.Get(lookupURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}

// handleItunesSearch godoc
// @Summary iTunes 搜尋（代理 Apple API）
// @Tags App
// @Produce json
// @Security BearerAuth
// @Param term query string true "搜尋關鍵字"
// @Param limit query int false "結果數量上限" default(10)
// @Param entity query string false "Apple entity (software / iPadSoftware / macSoftware / tvSoftware). Default: software"
// @Success 200 {object} map[string]interface{}
// @Router /api/itunes-search [get]
func (c *AppController) handleItunesSearch(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireModule(r, "mdm", "viewer"); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	term := r.URL.Query().Get("term")
	if term == "" {
		writeError(w, http.StatusBadRequest, "term required")
		return
	}
	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "10"
	}
	// Allowlist the entity value so we don't blindly forward arbitrary URL fragments to Apple.
	entity := r.URL.Query().Get("entity")
	switch entity {
	case "software", "iPadSoftware", "macSoftware", "tvSoftware":
		// ok
	default:
		entity = "software" // default to iOS/iPadOS universal apps
	}
	searchURL := fmt.Sprintf("https://itunes.apple.com/search?term=%s&country=tw&entity=%s&limit=%s",
		url.QueryEscape(term), entity, limit)
	resp, err := http.Get(searchURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}

// buildInstallPayload creates the MDM command payload for installing an app.
func (c *AppController) buildInstallPayload(r *http.Request, udid string, app *domain.ManagedApp) (map[string]interface{}, error) {
	if app.AppType == "enterprise" {
		return map[string]interface{}{
			"udid": udid, "request_type": "InstallEnterpriseApplication", "manifest_url": app.ManifestURL,
		}, nil
	}

	// VPP app
	if c.vppClient != nil && app.ItunesStoreID != "" {
		dev, devErr := c.deviceRepo.GetByUDID(r.Context(), udid)
		if devErr != nil || dev.SerialNumber == "" {
			return nil, fmt.Errorf("無法取得裝置序號，無法指派 VPP 授權")
		}
		vppResp, vppErr := c.vppClient.AssignLicense(r.Context(), app.ItunesStoreID, []string{dev.SerialNumber})
		if vppErr != nil {
			log.Printf("VPP assign license failed: %v", vppErr)
			return nil, fmt.Errorf("VPP 授權指派失敗: %v", vppErr)
		}
		log.Printf("VPP assign license response: %s", vppResp)
		var vppResult struct {
			Status       int    `json:"status"`
			ErrorNumber  int    `json:"errorNumber"`
			ErrorMessage string `json:"errorMessage"`
		}
		if json.Unmarshal([]byte(vppResp), &vppResult) == nil && vppResult.Status != 0 {
			return nil, fmt.Errorf("VPP 授權指派失敗 (error %d): %s", vppResult.ErrorNumber, vppResult.ErrorMessage)
		}
	}
	storeIDInt, parseErr := strconv.ParseInt(app.ItunesStoreID, 10, 64)
	if parseErr != nil {
		return nil, fmt.Errorf("invalid itunes_store_id: %s", app.ItunesStoreID)
	}
	return map[string]interface{}{
		"udid": udid, "request_type": "InstallApplication",
		"itunes_store_id": storeIDInt,
		"options":         map[string]interface{}{"purchase_method": 1},
	}, nil
}
