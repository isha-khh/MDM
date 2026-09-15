package controller

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/port"
)

type AssetController struct {
	assetRepo    port.AssetRepository
	auditRepo    port.AuditRepository
	custodyRepo  port.CustodyRepository
	userRepo     port.UserRepository
	categoryRepo port.CategoryRepository
	auth         *middleware.AuthHelper
}

func NewAssetController(assetRepo port.AssetRepository, auditRepo port.AuditRepository, custodyRepo port.CustodyRepository, userRepo port.UserRepository, categoryRepo port.CategoryRepository, auth *middleware.AuthHelper) *AssetController {
	return &AssetController{
		assetRepo: assetRepo, auditRepo: auditRepo,
		custodyRepo: custodyRepo, userRepo: userRepo,
		categoryRepo: categoryRepo, auth: auth,
	}
}

func (c *AssetController) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/assets", c.handleAssets)
	mux.HandleFunc("/api/assets-export", c.handleExport)
	mux.HandleFunc("/api/assets-template", c.handleTemplate)
	mux.HandleFunc("/api/assets-import", c.handleImport)
	mux.HandleFunc("/api/assets-lifecycle", c.handleLifecycle)
	mux.HandleFunc("/api/assets-custody", c.handleCustody)
	mux.HandleFunc("/api/assets-custody/", c.handleCustodyHistory)
	mux.HandleFunc("/api/assets/", c.handleAssetByID)
	mux.HandleFunc("/api/assets-batch-update", c.handleBatchUpdate)
	mux.HandleFunc("/api/device-status", c.handleDeviceStatus)
	mux.HandleFunc("/api/pickable-assets", c.handlePickableAssets)
}

func assetToRow(a *domain.Asset) map[string]interface{} {
	row := map[string]interface{}{
		"id": a.ID, "device_udid": a.DeviceUdid, "asset_number": a.AssetNumber,
		"name": a.Name, "spec": a.Spec, "quantity": a.Quantity, "unit": a.Unit,
		"unit_price": a.UnitPrice, "purpose": a.Purpose,
		"custodian_id": a.CustodianID, "custodian_name": a.CustodianName,
		"location": a.Location, "asset_category": a.AssetCategory, "notes": a.Notes,
		"is_rentable": a.IsRentable,
		"created_at":  a.CreatedAt.Format(time.RFC3339), "updated_at": a.UpdatedAt.Format(time.RFC3339),
		"device_name": a.DeviceName, "device_serial": a.DeviceSerial,
		"category_id": a.CategoryID, "category_name": a.CategoryName, "asset_status": a.AssetStatus,
		"dispose_reason": a.DisposeReason, "transferred_to": a.TransferredTo,
	}
	if a.AcquiredDate != nil {
		row["acquired_date"] = a.AcquiredDate.Format("2006-01-02")
	} else {
		row["acquired_date"] = nil
	}
	if a.AssignedDate != nil {
		row["assigned_date"] = a.AssignedDate.Format("2006-01-02")
	} else {
		row["assigned_date"] = nil
	}
	row["current_holder_id"] = a.CurrentHolderID
	row["current_holder_name"] = a.CurrentHolderName
	if a.CurrentHolderSince != nil {
		row["current_holder_since"] = a.CurrentHolderSince.Format(time.RFC3339)
	} else {
		row["current_holder_since"] = nil
	}
	if a.DisposedAt != nil {
		row["disposed_at"] = a.DisposedAt.Format(time.RFC3339)
	} else {
		row["disposed_at"] = nil
	}
	if a.TransferredAt != nil {
		row["transferred_at"] = a.TransferredAt.Format(time.RFC3339)
	} else {
		row["transferred_at"] = nil
	}
	row["disposed_by"] = a.DisposedBy
	return row
}

// handleAssets godoc
// @Summary 資產列表 / 新增資產
// @Tags Asset
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param device_udid query string false "依裝置 UDID 篩選" Example(devices-list)
// @Param body body swagAssetReq false "新增資產（POST）"
// @Success 200 {object} map[string]interface{} "GET: {assets: [...]}, POST: {id: ...}"
// @Failure 400 {object} swagError
// @Router /api/assets [get]
// @Router /api/assets [post]
func (c *AssetController) handleAssets(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireModule(r, "asset", "viewer"); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		deviceUdid := r.URL.Query().Get("device_udid")
		assets, err := c.assetRepo.List(r.Context(), deviceUdid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rows := make([]map[string]interface{}, 0, len(assets))
		for _, a := range assets {
			rows = append(rows, assetToRow(a))
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"assets": rows})

	case http.MethodPost:
		var body struct {
			DeviceUdid    *string `json:"device_udid"`
			AssetNumber   string  `json:"asset_number"`
			Name          string  `json:"name"`
			Spec          string  `json:"spec"`
			Quantity      int     `json:"quantity"`
			Unit          string  `json:"unit"`
			AcquiredDate  *string `json:"acquired_date"`
			UnitPrice     float64 `json:"unit_price"`
			Purpose       string  `json:"purpose"`
			Location      string  `json:"location"`
			AssetCategory string  `json:"asset_category"`
			Notes         string  `json:"notes"`
			CategoryID    *string `json:"category_id"`
			IsRentable    bool    `json:"is_rentable"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// Note: custodian and assigned_date are NOT accepted here.
		// Use POST /api/assets-custody to assign a custodian.
		asset := &domain.Asset{
			DeviceUdid: body.DeviceUdid, AssetNumber: body.AssetNumber,
			Name: body.Name, Spec: body.Spec, Quantity: body.Quantity, Unit: body.Unit,
			UnitPrice: body.UnitPrice, Purpose: body.Purpose,
			Location: body.Location, AssetCategory: body.AssetCategory, Notes: body.Notes,
			CategoryID: body.CategoryID, IsRentable: body.IsRentable,
		}
		id, err := c.assetRepo.Create(r.Context(), asset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"id": id})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleAssetByID godoc
// @Summary 更新 / 刪除資產
// @Tags Asset
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "資產 ID"
// @Param body body map[string]interface{} false "更新欄位（PUT）"
// @Success 200 {object} swagOK
// @Router /api/assets/{id} [put]
// @Router /api/assets/{id} [delete]
func (c *AssetController) handleAssetByID(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireModule(r, "asset", "operator"); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/assets/")
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
		if err := c.assetRepo.Update(r.Context(), id, body); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeOK(w)

	case http.MethodDelete:
		if err := c.assetRepo.Delete(r.Context(), id); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeOK(w)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleBatchUpdate godoc
// @Summary 批次更新資產（分類、存放地點、用途、可租借）
// @Tags Asset
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body swagAssetBatchUpdateReq true "資產 ID 清單與要更新的欄位"
// @Success 200 {object} map[string]interface{} "{updated, errors}"
// @Failure 400 {object} swagError
// @Router /api/assets-batch-update [post]
func (c *AssetController) handleBatchUpdate(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireModule(r, "asset", "operator")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var body struct {
		IDs    []string               `json:"ids"`
		Fields map[string]interface{} `json:"fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 || len(body.Fields) == 0 {
		writeError(w, http.StatusBadRequest, "ids and fields required")
		return
	}

	// Batch edit is intentionally restricted to a handful of low-risk fields.
	// Custodian changes go through /api/assets-custody (audited transfer
	// log) and disposal/transfer through /api/assets-lifecycle (audited,
	// one-way state transitions) — letting this generic endpoint touch
	// either would silently bypass those compliance trails.
	allowed := map[string]bool{"category_id": true, "location": true, "purpose": true, "is_rentable": true}
	filtered := map[string]interface{}{}
	for k, v := range body.Fields {
		if allowed[k] {
			filtered[k] = v
		}
	}
	if len(filtered) == 0 {
		writeError(w, http.StatusBadRequest, "no editable fields provided")
		return
	}

	updated := 0
	var errs []string
	for _, id := range body.IDs {
		if err := c.assetRepo.Update(r.Context(), id, filtered); err != nil {
			errs = append(errs, id+": "+err.Error())
			continue
		}
		updated++
	}

	c.auditRepo.Create(r.Context(), &domain.AuditLog{
		UserID: claims.UserID, Username: claims.Username,
		Action: "asset_batch_update", Target: strings.Join(body.IDs, ","),
		Detail: fmt.Sprintf("fields=%v count=%d", filtered, updated), Module: "asset",
		IPAddress: clientIP(r), UserAgent: r.UserAgent(),
	})

	writeJSON(w, map[string]interface{}{"updated": updated, "errors": errs})
}

// handleDeviceStatus godoc
// @Summary 更新裝置資產狀態
// @Tags Asset
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body swagDeviceStatusReq true "UDID + 狀態"
// @Success 200 {object} swagOK
// @Failure 400 {object} swagError
// @Router /api/device-status [put]
func (c *AssetController) handleDeviceStatus(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireModule(r, "asset", "operator"); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPut) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var body struct {
		UDID   string `json:"udid"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.UDID == "" || body.Status == "" {
		writeError(w, http.StatusBadRequest, "udid and status required")
		return
	}
	valid := map[string]bool{"available": true, "faulty": true, "repairing": true, "retired": true, "transferred": true}
	if !valid[body.Status] {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if err := c.assetRepo.UpdateStatus(r.Context(), body.UDID, body.Status); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w)
}

// handlePickableAssets godoc
// @Summary 可選資產列表（不篩選可租借狀態，供維修/報廢申請等表單挑選資產用）
// @Tags Asset
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Router /api/pickable-assets [get]
func (c *AssetController) handlePickableAssets(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireAuth(r); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	items, err := c.assetRepo.ListPickable(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type row struct {
		AssetID      string  `json:"asset_id"`
		AssetNumber  string  `json:"asset_number"`
		Name         string  `json:"name"`
		Spec         string  `json:"spec"`
		DeviceUdid   *string `json:"device_udid"`
		SerialNumber string  `json:"serial_number"`
		Model        string  `json:"model"`
		OSVersion    string  `json:"os_version"`
		AssetStatus  string  `json:"asset_status"`
		CategoryID   *string `json:"category_id"`
		CategoryName string  `json:"category_name"`
	}
	rows := make([]row, 0, len(items))
	for _, it := range items {
		rows = append(rows, row{
			AssetID: it.AssetID, AssetNumber: it.AssetNumber, Name: it.Name, Spec: it.Spec,
			DeviceUdid:   it.DeviceUdid,
			SerialNumber: it.SerialNumber, Model: it.Model, OSVersion: it.OSVersion,
			AssetStatus: it.AssetStatus, CategoryID: it.CategoryID, CategoryName: it.CategoryName,
		})
	}
	writeJSON(w, map[string]interface{}{"assets": rows})
}

// handleLifecycle handles asset lifecycle transitions (dispose / transfer).
// @Summary 資產生命週期操作（報廢/移撥）
// @Tags Asset
// @Accept json
// @Produce json
// @Security BearerAuth
// @Router /api/assets-lifecycle [post]
func (c *AssetController) handleLifecycle(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireModule(r, "asset", "manager")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var body struct {
		Action        string `json:"action"` // "dispose" or "transfer"
		AssetID       string `json:"asset_id"`
		Reason        string `json:"reason"`         // for dispose
		TransferredTo string `json:"transferred_to"` // for transfer
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AssetID == "" || body.Action == "" {
		writeError(w, http.StatusBadRequest, "action and asset_id required")
		return
	}

	switch body.Action {
	case "dispose":
		if err := c.assetRepo.Dispose(r.Context(), body.AssetID, claims.UserID, body.Reason); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		c.auditRepo.Create(r.Context(), &domain.AuditLog{
			UserID: claims.UserID, Username: claims.Username,
			Action: "asset_dispose", Target: body.AssetID,
			Detail: body.Reason, Module: "asset",
			IPAddress: clientIP(r), UserAgent: r.UserAgent(),
		})

	case "transfer":
		if body.TransferredTo == "" {
			writeError(w, http.StatusBadRequest, "transferred_to required")
			return
		}
		if err := c.assetRepo.Transfer(r.Context(), body.AssetID, body.TransferredTo); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		c.auditRepo.Create(r.Context(), &domain.AuditLog{
			UserID: claims.UserID, Username: claims.Username,
			Action: "asset_transfer", Target: body.AssetID,
			Detail: "transferred to: " + body.TransferredTo, Module: "asset",
			IPAddress: clientIP(r), UserAgent: r.UserAgent(),
		})

	default:
		writeError(w, http.StatusBadRequest, "action must be 'dispose' or 'transfer'")
		return
	}

	writeOK(w)
}

// handleCustody godoc
// @Summary 保管權指派 / 移轉 / 收回
// @Tags Asset
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body swagCustodyReq true "action=assign|transfer|revoke"
// @Success 200 {object} swagOK
// @Failure 400 {object} swagError
// @Failure 409 {object} swagError
// @Router /api/assets-custody [post]
func (c *AssetController) handleCustody(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireModule(r, "asset", "manager")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var body struct {
		Action   string `json:"action"`
		AssetID  string `json:"asset_id"`
		ToUserID string `json:"to_user_id"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AssetID == "" || body.Action == "" {
		writeError(w, http.StatusBadRequest, "action and asset_id required")
		return
	}
	if body.Action != "assign" && body.Action != "transfer" && body.Action != "revoke" {
		writeError(w, http.StatusBadRequest, "action must be assign, transfer or revoke")
		return
	}
	if (body.Action == "assign" || body.Action == "transfer") && body.ToUserID == "" {
		writeError(w, http.StatusBadRequest, "to_user_id required")
		return
	}

	asset, err := c.assetRepo.GetByID(r.Context(), body.AssetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "asset not found")
		return
	}
	if asset.AssetStatus == "retired" || asset.AssetStatus == "transferred" {
		writeError(w, http.StatusConflict, "asset is retired or transferred")
		return
	}
	if asset.CurrentHolderID != nil && *asset.CurrentHolderID != "" {
		writeError(w, http.StatusConflict, "asset is currently rented out; must be returned before custody change")
		return
	}

	var (
		toID      *string
		toName    string
		newAssign *time.Time
		auditActn string
	)

	switch body.Action {
	case "assign", "transfer":
		u, err := c.userRepo.GetByID(r.Context(), body.ToUserID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "to_user not found")
			return
		}
		name := u.DisplayName
		if name == "" {
			name = u.Username
		}
		id := u.ID
		toID = &id
		toName = name
		now := time.Now()
		newAssign = &now
		if asset.CustodianID == nil || *asset.CustodianID == "" {
			auditActn = "custody_assign"
		} else {
			auditActn = "custody_transfer"
		}

	case "revoke":
		if asset.CustodianID == nil || *asset.CustodianID == "" {
			writeError(w, http.StatusConflict, "asset has no custodian to revoke")
			return
		}
		toID = nil
		toName = ""
		newAssign = nil
		auditActn = "custody_revoke"
	}

	if err := c.assetRepo.SetCustodian(r.Context(), asset.ID, toID, toName, newAssign); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	operatorID := claims.UserID
	logAction := body.Action
	if logAction == "assign" && asset.CustodianID != nil && *asset.CustodianID != "" {
		logAction = "transfer"
	}
	custodyLog := &domain.AssetCustodyLog{
		AssetID:      asset.ID,
		Action:       logAction,
		FromUserID:   asset.CustodianID,
		FromUserName: asset.CustodianName,
		ToUserID:     toID,
		ToUserName:   toName,
		Reason:       body.Reason,
		OperatedBy:   &operatorID,
		OperatorName: claims.Username,
	}
	if err := c.custodyRepo.Append(r.Context(), custodyLog); err != nil {
		log.Printf("[custody] append log failed: %v", err)
	}

	detail := fmt.Sprintf("from=%s to=%s reason=%s", asset.CustodianName, toName, body.Reason)
	c.auditRepo.Create(r.Context(), &domain.AuditLog{
		UserID: claims.UserID, Username: claims.Username,
		Action: auditActn, Target: asset.ID, Detail: detail, Module: "asset",
		IPAddress: clientIP(r), UserAgent: r.UserAgent(),
	})

	writeOK(w)
}

// handleCustodyHistory godoc
// @Summary 查詢資產的保管權歷史
// @Tags Asset
// @Produce json
// @Security BearerAuth
// @Param asset_id path string true "資產 ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/assets-custody/{asset_id} [get]
func (c *AssetController) handleCustodyHistory(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireModule(r, "asset", "viewer"); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	assetID := strings.TrimPrefix(r.URL.Path, "/api/assets-custody/")
	if assetID == "" {
		writeError(w, http.StatusBadRequest, "asset_id required")
		return
	}
	logs, err := c.custodyRepo.ListByAsset(r.Context(), assetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows := make([]map[string]interface{}, 0, len(logs))
	for _, l := range logs {
		rows = append(rows, map[string]interface{}{
			"id":             l.ID,
			"asset_id":       l.AssetID,
			"action":         l.Action,
			"from_user_id":   l.FromUserID,
			"from_user_name": l.FromUserName,
			"to_user_id":     l.ToUserID,
			"to_user_name":   l.ToUserName,
			"reason":         l.Reason,
			"operated_by":    l.OperatedBy,
			"operator_name":  l.OperatorName,
			"created_at":     l.CreatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, map[string]interface{}{"logs": rows})
}

// importHeaders defines the column order for the import template. The import
// parser matches columns by header name (first row), so column order can vary
// as long as the header cells match exactly.
var importHeaders = []string{
	"財產編號", "名稱", "規格", "數量", "單位", "單價", "取得日期",
	"存放處所", "分類", "用途", "備註",
}

// handleTemplate returns a blank Excel template for bulk import.
// @Summary 下載財產匯入 Excel 範本
// @Tags Asset
// @Produce application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Security BearerAuth
// @Success 200 {file} file "Excel 範本"
// @Router /api/assets-template [get]
func (c *AssetController) handleTemplate(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireModule(r, "asset", "operator"); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	f := excelize.NewFile()
	defer f.Close()

	// Sheet 1: data
	dataSheet := "財產清冊"
	f.SetSheetName("Sheet1", dataSheet)
	for col, h := range importHeaders {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		f.SetCellValue(dataSheet, cell, h)
	}
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "#FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#2563EB"}},
	})
	endCell, _ := excelize.CoordinatesToCellName(len(importHeaders), 1)
	f.SetCellStyle(dataSheet, "A1", endCell, headerStyle)

	// Example row to guide the user
	example := []interface{}{
		"A-2025-0001", "MacBook Air 15", "M3/16G/512G", 1, "台", 42900,
		"2025-01-15", "3F 辦公室", "筆記型電腦", "行政使用", "",
	}
	for col, v := range example {
		cell, _ := excelize.CoordinatesToCellName(col+1, 2)
		f.SetCellValue(dataSheet, cell, v)
	}
	for col := range importHeaders {
		name, _ := excelize.ColumnNumberToName(col + 1)
		f.SetColWidth(dataSheet, name, name, 16)
	}

	// Sheet 2: instructions
	instr := "說明"
	f.NewSheet(instr)
	instructions := [][]interface{}{
		{"欄位", "必填", "說明"},
		{"財產編號", "否", "留空將以系統預設值建立；若已存在則視為更新"},
		{"名稱", "是", "財產名稱"},
		{"規格", "否", "型號或規格說明"},
		{"數量", "否", "整數，預設 1"},
		{"單位", "否", "例：台、支、套"},
		{"單價", "否", "數字，元"},
		{"取得日期", "否", "格式 YYYY-MM-DD"},
		{"存放處所", "否", "所在位置"},
		{"分類", "否", "系統會以名稱對應分類；名稱需與「資產品分類」一致"},
		{"用途", "否", "使用目的"},
		{"備註", "否", "任意說明"},
		{"", "", ""},
		{"注意事項", "", ""},
		{"", "", "保管人 / 目前持有人 / 狀態不可由匯入設定，需透過系統操作流程變更。"},
		{"", "", "若提供「財產編號」且系統已存在相同編號，該列將以更新方式寫入。"},
	}
	for i, row := range instructions {
		for j, v := range row {
			cell, _ := excelize.CoordinatesToCellName(j+1, i+1)
			f.SetCellValue(instr, cell, v)
		}
	}
	f.SetColWidth(instr, "A", "A", 14)
	f.SetColWidth(instr, "B", "B", 8)
	f.SetColWidth(instr, "C", "C", 60)
	tblEnd, _ := excelize.CoordinatesToCellName(3, 1)
	f.SetCellStyle(instr, "A1", tblEnd, headerStyle)

	f.SetActiveSheet(0)

	filename := fmt.Sprintf("財產匯入範本_%s.xlsx", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	if err := f.Write(w); err != nil {
		log.Printf("[asset-template] write error: %v", err)
	}
}

// handleImport parses an uploaded Excel file and creates/updates asset rows.
// @Summary 匯入財產清冊 Excel
// @Tags Asset
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param file formData file true "Excel 檔案 (.xlsx)"
// @Success 200 {object} map[string]interface{} "{created, updated, failed, errors}"
// @Router /api/assets-import [post]
func (c *AssetController) handleImport(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireModule(r, "asset", "operator")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file required")
		return
	}
	defer file.Close()

	f, err := excelize.OpenReader(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid Excel file")
		return
	}
	defer f.Close()

	sheet := f.GetSheetList()[0]
	rows, err := f.GetRows(sheet)
	if err != nil {
		writeError(w, http.StatusBadRequest, "cannot read sheet: "+err.Error())
		return
	}
	if len(rows) < 2 {
		writeJSON(w, map[string]interface{}{
			"created": 0, "updated": 0, "failed": 0, "errors": []string{},
		})
		return
	}

	// Build header → column index map.
	header := rows[0]
	colIdx := map[string]int{}
	for i, h := range header {
		colIdx[strings.TrimSpace(h)] = i
	}
	required := []string{"名稱"}
	for _, req := range required {
		if _, ok := colIdx[req]; !ok {
			writeError(w, http.StatusBadRequest, "缺少必要欄位: "+req)
			return
		}
	}

	// Build category name → id lookup.
	categories, _ := c.categoryRepo.List(r.Context())
	catByName := map[string]string{}
	for _, cat := range categories {
		catByName[strings.TrimSpace(cat.Name)] = cat.ID
	}

	getCell := func(row []string, name string) string {
		i, ok := colIdx[name]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	var (
		created, updated, failed int
		errs                     []string
	)

	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) == 0 {
			continue
		}
		name := getCell(row, "名稱")
		if name == "" {
			// skip fully-empty rows silently; flag otherwise
			nonEmpty := false
			for _, cell := range row {
				if strings.TrimSpace(cell) != "" {
					nonEmpty = true
					break
				}
			}
			if nonEmpty {
				failed++
				errs = append(errs, fmt.Sprintf("第 %d 列：名稱為必填", i+1))
			}
			continue
		}

		qty := 1
		if s := getCell(row, "數量"); s != "" {
			if v, err := strconv.Atoi(s); err == nil {
				qty = v
			}
		}
		price := 0.0
		if s := getCell(row, "單價"); s != "" {
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				price = v
			}
		}
		var acquired *time.Time
		if s := getCell(row, "取得日期"); s != "" {
			if t, err := time.Parse("2006-01-02", s); err == nil {
				acquired = &t
			} else if t, err := time.Parse("2006/1/2", s); err == nil {
				acquired = &t
			} else {
				failed++
				errs = append(errs, fmt.Sprintf("第 %d 列：取得日期格式錯誤 (%s)", i+1, s))
				continue
			}
		}

		var catID *string
		if s := getCell(row, "分類"); s != "" {
			if id, ok := catByName[s]; ok {
				catID = &id
			} else {
				failed++
				errs = append(errs, fmt.Sprintf("第 %d 列：找不到分類「%s」", i+1, s))
				continue
			}
		}

		assetNum := getCell(row, "財產編號")
		asset := &domain.Asset{
			AssetNumber:   assetNum,
			Name:          name,
			Spec:          getCell(row, "規格"),
			Quantity:      qty,
			Unit:          getCell(row, "單位"),
			AcquiredDate:  acquired,
			UnitPrice:     price,
			Purpose:       getCell(row, "用途"),
			Location:      getCell(row, "存放處所"),
			AssetCategory: getCell(row, "分類"),
			Notes:         getCell(row, "備註"),
			CategoryID:    catID,
		}

		id, err := c.assetRepo.Create(r.Context(), asset)
		if err != nil {
			failed++
			errs = append(errs, fmt.Sprintf("第 %d 列：建立失敗 (%s)", i+1, err.Error()))
			continue
		}
		created++

		c.auditRepo.Create(r.Context(), &domain.AuditLog{
			UserID: claims.UserID, Username: claims.Username,
			Action: "asset_import", Target: id,
			Detail:    fmt.Sprintf("imported: %s / %s", assetNum, name),
			Module:    "asset",
			IPAddress: clientIP(r), UserAgent: r.UserAgent(),
		})
	}

	writeJSON(w, map[string]interface{}{
		"created": created, "updated": updated, "failed": failed, "errors": errs,
	})
}

// handleExport exports the asset list as Excel.
// @Summary 匯出財產清冊為 Excel
// @Tags Asset
// @Produce application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Security BearerAuth
// @Success 200 {file} file "Excel 檔案"
// @Router /api/assets-export [get]
func (c *AssetController) handleExport(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireModule(r, "asset", "viewer"); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	assets, err := c.assetRepo.List(r.Context(), "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	statusLabels := map[string]string{
		"available": "可用", "faulty": "故障", "repairing": "維修中",
		"retired": "報廢", "transferred": "移撥", "lost": "遺失",
	}

	f := excelize.NewFile()
	sheet := "財產清冊"
	f.SetSheetName("Sheet1", sheet)

	headers := []string{
		"財產編號", "名稱", "規格", "數量", "單位", "單價", "取得日期",
		"保管人", "存放處所", "分類", "狀態", "用途", "裝置名稱", "裝置序號",
		"報廢原因", "移撥對象", "備註",
	}
	for col, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		f.SetCellValue(sheet, cell, h)
	}
	style, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	endCell, _ := excelize.CoordinatesToCellName(len(headers), 1)
	f.SetCellStyle(sheet, "A1", endCell, style)

	for i, a := range assets {
		row := i + 2
		acquiredDate := ""
		if a.AcquiredDate != nil {
			acquiredDate = a.AcquiredDate.Format("2006-01-02")
		}
		statusLabel := statusLabels[a.AssetStatus]
		if statusLabel == "" {
			statusLabel = a.AssetStatus
		}

		vals := []interface{}{
			a.AssetNumber, a.Name, a.Spec, a.Quantity, a.Unit, a.UnitPrice, acquiredDate,
			a.CustodianName, a.Location, a.CategoryName, statusLabel, a.Purpose,
			a.DeviceName, a.DeviceSerial,
			a.DisposeReason, a.TransferredTo, a.Notes,
		}
		for col, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(col+1, row)
			f.SetCellValue(sheet, cell, v)
		}
	}

	for col := range headers {
		colName, _ := excelize.ColumnNumberToName(col + 1)
		f.SetColWidth(sheet, colName, colName, 14)
	}

	now := time.Now().Format("2006-01-02")
	filename := fmt.Sprintf("財產清冊_%s.xlsx", now)

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	if err := f.Write(w); err != nil {
		log.Printf("[asset-export] write error: %v", err)
	}
	f.Close()
}
