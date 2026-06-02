package controller

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/mdm-server/internal/adapter/postgres"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/port"
)

type DeviceController struct {
	deviceRepo   *postgres.DeviceRepo
	mdmClient    port.MicroMDMClient
	auth         *middleware.AuthHelper
	depScheduler port.DEPSchedulerRunner // nil when DEP_AUTO_ASSIGN=false
}

func NewDeviceController(deviceRepo *postgres.DeviceRepo, mdmClient port.MicroMDMClient, auth *middleware.AuthHelper, depScheduler port.DEPSchedulerRunner) *DeviceController {
	return &DeviceController{deviceRepo: deviceRepo, mdmClient: mdmClient, auth: auth, depScheduler: depScheduler}
}

func (c *DeviceController) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/devices/", c.handleDeviceByID)
	mux.HandleFunc("/api/devices-list", c.handleDevicesList)
	mux.HandleFunc("/api/devices-available", c.handleDevicesAvailable)
	mux.HandleFunc("/api/sync-device-info", c.handleSyncDeviceInfo)
	mux.HandleFunc("/api/dep/apply-now", c.handleDEPApplyNow)
}

// handleDEPApplyNow runs the DEP scheduler's RunOnce synchronously so an admin
// can apply profiles to brand-new ABM devices without waiting for the next
// polling tick. Requires DEP_AUTO_ASSIGN=true at boot (so the scheduler exists).
//
// @Summary 立即套用 DEP profile（觸發排程器跑一次）
// @Tags Device
// @Produce json
// @Security BearerAuth
// @Success 200 {object} swagOK
// @Failure 503 {object} swagError "DEP 排程器未啟用"
// @Router /api/dep/apply-now [post]
func (c *DeviceController) handleDEPApplyNow(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireSysAdmin(r); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if c.depScheduler == nil {
		writeError(w, http.StatusServiceUnavailable, "DEP 自動派發未啟用（DEP_AUTO_ASSIGN=false 或 ABM 設定不完整）")
		return
	}
	// RunOnce logs counts but doesn't return them. Fire-and-acknowledge —
	// the admin can refresh the page or look at logs for details.
	c.depScheduler.RunOnce(r.Context())
	writeJSON(w, map[string]interface{}{"ok": true, "message": "DEP 套用完成，請查看伺服器 log 取得明細"})
	log.Printf("[dep-apply-now] triggered by admin")
}

// handleDeviceByID godoc
// @Summary 依 UDID 取得裝置詳細資訊
// @Tags Device
// @Produce json
// @Security BearerAuth
// @Param udid path string true "裝置 UDID"
// @Success 200 {object} swagDevice
// @Failure 404 {object} swagError
// @Router /api/devices/{udid} [get]
func (c *DeviceController) handleDeviceByID(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireModule(r, "mdm", "viewer"); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	udid := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	if udid == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	d, err := c.deviceRepo.GetByUDID(r.Context(), udid)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, map[string]interface{}{
		"udid":              d.UDID,
		"serial_number":     d.SerialNumber,
		"device_name":       d.DeviceName,
		"model":             d.Model,
		"os_version":        d.OSVersion,
		"last_seen":         d.LastSeen.Format(time.RFC3339),
		"enrollment_status": d.EnrollmentStatus,
		"is_supervised":     d.IsSupervised,
		"is_lost_mode":      d.IsLostMode,
		"battery_level":     d.BatteryLevel,
		"details":           d.Details,
	})
}

// handleDevicesList godoc
// @Summary 裝置列表（含資產與借用狀態）
// @Tags Device
// @Produce json
// @Security BearerAuth
// @Param filter query string false "關鍵字篩選"
// @Param category_id query string false "分類 ID"
// @Param custodian_id query string false "保管人 ID"
// @Param rental_status query string false "借用狀態"
// @Success 200 {object} swagDeviceListResp
// @Router /api/devices-list [get]
func (c *DeviceController) handleDevicesList(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireModule(r, "mdm", "viewer")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	filter := r.URL.Query().Get("filter")
	category := r.URL.Query().Get("category_id")
	custodian := r.URL.Query().Get("custodian_id")
	rentalStatus := r.URL.Query().Get("rental_status")

	var viewerUserID string
	if claims.Role == "viewer" {
		viewerUserID = claims.UserID
	}

	devices, err := c.deviceRepo.ListWithAssets(r.Context(), filter, category, custodian, rentalStatus, viewerUserID)
	if err != nil {
		log.Printf("devices-list: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	type deviceRow struct {
		UDID             string  `json:"udid"`
		SerialNumber     string  `json:"serial_number"`
		DeviceName       string  `json:"device_name"`
		Model            string  `json:"model"`
		OSVersion        string  `json:"os_version"`
		LastSeen         string  `json:"last_seen"`
		EnrollmentStatus string  `json:"enrollment_status"`
		IsSupervised     bool    `json:"is_supervised"`
		IsLostMode       bool    `json:"is_lost_mode"`
		BatteryLevel     float64 `json:"battery_level"`
		CustodianName    string  `json:"custodian_name"`
		CategoryName     string  `json:"category_name"`
		CategoryID       *string `json:"category_id"`
		CustodianID      *string `json:"custodian_id"`
		AssetStatus      string  `json:"asset_status"`
	}

	rows := make([]deviceRow, 0, len(devices))
	for _, d := range devices {
		rows = append(rows, deviceRow{
			UDID: d.UDID, SerialNumber: d.SerialNumber, DeviceName: d.DeviceName,
			Model: d.Model, OSVersion: d.OSVersion, LastSeen: d.LastSeen.Format(time.RFC3339),
			EnrollmentStatus: d.EnrollmentStatus, IsSupervised: d.IsSupervised,
			IsLostMode: d.IsLostMode, BatteryLevel: d.BatteryLevel,
			CustodianName: d.CustodianName, CategoryName: d.CategoryName,
			CategoryID: d.CategoryID, CustodianID: d.CustodianID, AssetStatus: d.AssetStatus,
		})
	}
	writeJSON(w, map[string]interface{}{"devices": rows, "total": len(rows)})
}

// handleDevicesAvailable godoc
// @Summary 可借用裝置列表
// @Tags Device
// @Produce json
// @Security BearerAuth
// @Success 200 {object} swagDeviceAvailResp
// @Router /api/devices-available [get]
func (c *DeviceController) handleDevicesAvailable(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireAuth(r); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	devices, err := c.deviceRepo.ListAvailable(r.Context())
	if err != nil {
		log.Printf("devices-available: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	type deviceRow struct {
		UDID             string  `json:"udid"`
		SerialNumber     string  `json:"serial_number"`
		DeviceName       string  `json:"device_name"`
		Model            string  `json:"model"`
		OSVersion        string  `json:"os_version"`
		EnrollmentStatus string  `json:"enrollment_status"`
		AssetStatus      string  `json:"asset_status"`
		CategoryID       *string `json:"category_id"`
		CategoryName     string  `json:"category_name"`
	}
	rows := make([]deviceRow, 0, len(devices))
	for _, d := range devices {
		rows = append(rows, deviceRow{
			UDID: d.UDID, SerialNumber: d.SerialNumber, DeviceName: d.DeviceName,
			Model: d.Model, OSVersion: d.OSVersion,
			EnrollmentStatus: d.EnrollmentStatus, AssetStatus: d.AssetStatus,
			CategoryID: d.CategoryID, CategoryName: d.CategoryName,
		})
	}
	writeJSON(w, map[string]interface{}{"devices": rows})
}

// handleSyncDeviceInfo godoc
// @Summary 向所有裝置發送 DeviceInformation 查詢
// @Tags Device
// @Produce json
// @Security BearerAuth
// @Success 200 {object} swagSyncCountResp
// @Failure 401 {string} string "Unauthorized"
// @Router /api/sync-device-info [post]
func (c *DeviceController) handleSyncDeviceInfo(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireModule(r, "mdm", "manager"); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	devices, _, err := c.deviceRepo.List(r.Context(), "", 500, 0)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	queries := []string{
		"UDID", "DeviceName", "OSVersion", "BuildVersion",
		"ModelName", "Model", "ProductName", "SerialNumber",
		"DeviceCapacity", "AvailableDeviceCapacity", "BatteryLevel",
		"IsSupervised", "IsActivationLockEnabled", "IsMDMLostModeEnabled",
		"WiFiMAC", "BluetoothMAC",
	}
	count := 0
	for _, d := range devices {
		payload := map[string]interface{}{
			"udid": d.UDID, "request_type": "DeviceInformation", "queries": queries,
		}
		if _, err := c.mdmClient.SendCommand(r.Context(), payload); err != nil {
			continue
		}
		_ = c.mdmClient.SendPush(r.Context(), d.UDID)
		count++
	}
	writeJSON(w, map[string]interface{}{"count": count})
	log.Printf("[sync-info] sent DeviceInformation to %d devices", count)
}
