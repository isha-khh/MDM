package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/anthropics/mdm-server/internal/adapter/postgres"
	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/port"
	"github.com/anthropics/mdm-server/internal/service"
)

type RentalController struct {
	rentalRepo      *postgres.RentalRepo
	assetRepo       *postgres.AssetRepo
	userRepo        port.UserRepository
	notifySvc       *service.NotifyService
	auth            *middleware.AuthHelper
	categoryRepo    port.CategoryRepository
	rentalRuleRepo  *postgres.CategoryRentalRuleRepo
	dailyReportRepo *postgres.RentalDailyReportRepo
	templateRepo    *postgres.ChecklistTemplateRepo
}

func NewRentalController(rentalRepo *postgres.RentalRepo, assetRepo *postgres.AssetRepo, userRepo port.UserRepository, notifySvc *service.NotifyService, auth *middleware.AuthHelper, categoryRepo port.CategoryRepository, rentalRuleRepo *postgres.CategoryRentalRuleRepo, dailyReportRepo *postgres.RentalDailyReportRepo, templateRepo *postgres.ChecklistTemplateRepo) *RentalController {
	return &RentalController{
		rentalRepo: rentalRepo, assetRepo: assetRepo, userRepo: userRepo, notifySvc: notifySvc, auth: auth,
		categoryRepo: categoryRepo, rentalRuleRepo: rentalRuleRepo, dailyReportRepo: dailyReportRepo, templateRepo: templateRepo,
	}
}

// resolveDailyTrackingRequired reports whether any of the given assets'
// categories (walking up the inheritance chain) is flagged
// daily_tracking_required — a "union" match, same as the checklist-merge
// policy planned for Phase 2. categoryIDs may contain nils (standalone
// assets with no category), which are simply skipped.
func (c *RentalController) resolveDailyTrackingRequired(ctx context.Context, categoryIDs []*string) (bool, error) {
	hasCategory := false
	for _, cid := range categoryIDs {
		if cid != nil && *cid != "" {
			hasCategory = true
			break
		}
	}
	if !hasCategory {
		return false, nil
	}
	cats, err := c.categoryRepo.List(ctx)
	if err != nil {
		return false, err
	}
	for _, cid := range categoryIDs {
		if cid == nil || *cid == "" {
			continue
		}
		required, err := c.rentalRuleRepo.ResolveDailyTrackingRequired(ctx, cats, *cid)
		if err != nil {
			return false, err
		}
		if required {
			return true, nil
		}
	}
	return false, nil
}

// resolveDailyTrackingRequiredForRentalNumber is the same check, but for an
// already-created rental batch — used by the daily-report endpoint, which
// only has a rental ID (not the original create-time category list) to work
// from.
func (c *RentalController) resolveDailyTrackingRequiredForRentalNumber(ctx context.Context, rentalNumber int) (bool, error) {
	assetIDs, err := c.rentalRepo.ListAssetIDsByNumber(ctx, rentalNumber)
	if err != nil {
		return false, err
	}
	categoryIDs := make([]*string, 0, len(assetIDs))
	for _, aid := range assetIDs {
		a, err := c.assetRepo.GetByID(ctx, aid)
		if err != nil || a == nil {
			continue
		}
		categoryIDs = append(categoryIDs, a.CategoryID)
	}
	return c.resolveDailyTrackingRequired(ctx, categoryIDs)
}

// sameCalendarDay compares two times ignoring time-of-day/timezone offset
// within the day — used to decide whether a rental spans more than one day.
func sameCalendarDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// dateOnly truncates t to a bare calendar date (midnight, same location),
// for comparing DATE-typed values without time-of-day noise.
func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// formatChecklistValue renders one return_checklist answer for Excel export,
// using item's resolved type (if known) to pick a sensible representation.
// item may be the zero value when the key couldn't be resolved to any
// template (e.g. stale data from a since-edited template) — in that case it
// falls back to a generic string print.
func formatChecklistValue(item domain.ChecklistItem, value interface{}) string {
	if value == nil {
		return ""
	}
	switch item.Type {
	case "boolean":
		if b, ok := value.(bool); ok && b {
			return "V"
		}
		return ""
	case "number":
		s := fmt.Sprintf("%v", value)
		if item.Unit != "" {
			s += " " + item.Unit
		}
		return s
	case "location":
		if m, ok := value.(map[string]interface{}); ok {
			if addr, ok := m["address"].(string); ok && addr != "" {
				return addr
			}
			if lat, ok := m["lat"]; ok {
				return fmt.Sprintf("%v,%v", lat, m["lng"])
			}
		}
		return fmt.Sprintf("%v", value)
	case "photo":
		if arr, ok := value.([]interface{}); ok {
			return fmt.Sprintf("%d 張照片", len(arr))
		}
		return ""
	default:
		return fmt.Sprintf("%v", value)
	}
}

// buildNotifyData gathers device names and common fields for notification emails.
func (c *RentalController) buildNotifyData(ctx context.Context, rentalNumber int, borrowerName, approverName, purpose, notes string, expectedReturn *time.Time) service.RentalNotifyData {
	data := service.RentalNotifyData{
		RentalNumber: rentalNumber,
		BorrowerName: borrowerName,
		ApproverName: approverName,
		Purpose:      purpose,
		Notes:        notes,
	}
	if expectedReturn != nil {
		data.ExpectedReturn = expectedReturn.Format("2006-01-02")
	}
	// Gather device/asset names from all rentals with this number
	rentals, _ := c.rentalRepo.List(ctx, "", "", false)
	for _, rl := range rentals {
		if rl.RentalNumber == rentalNumber {
			name := rl.DeviceName
			if name == "" {
				name = rl.AssetName
			}
			if name == "" {
				name = rl.DeviceSerial
			}
			if name == "" {
				name = rl.AssetNumber
			}
			if name == "" {
				name = rl.DeviceUdid
			}
			data.DeviceNames = append(data.DeviceNames, name)
		}
	}
	return data
}

func (c *RentalController) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/rentals", c.handleRentals)
	mux.HandleFunc("/api/rentals-export", c.handleExport)
	mux.HandleFunc("/api/rentals/", c.handleRentalByID)
	mux.HandleFunc("/api/rentals-archive", c.handleArchive)
	mux.HandleFunc("/api/rental-pickable-assets", c.handlePickableAssets)
}

// handlePickableAssets godoc
// @Summary 可借用資產列表（含獨立資產）
// @Tags Rental
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Router /api/rental-pickable-assets [get]
func (c *RentalController) handlePickableAssets(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireAuth(r); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	items, err := c.assetRepo.ListRentalPickable(r.Context())
	if err != nil {
		log.Printf("rental-pickable-assets: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	type row struct {
		AssetID            string                 `json:"asset_id"`
		AssetNumber        string                 `json:"asset_number"`
		Name               string                 `json:"name"`
		Spec               string                 `json:"spec"`
		DeviceUdid         *string                `json:"device_udid"`
		SerialNumber       string                 `json:"serial_number"`
		Model              string                 `json:"model"`
		OSVersion          string                 `json:"os_version"`
		AssetStatus        string                 `json:"asset_status"`
		CategoryID         *string                `json:"category_id"`
		CategoryName       string                 `json:"category_name"`
		LastReturnLocation map[string]interface{} `json:"last_return_location,omitempty"`
		LastReturnAt       *string                `json:"last_return_at,omitempty"`
	}
	rows := make([]row, 0, len(items))
	for _, it := range items {
		rr := row{
			AssetID: it.AssetID, AssetNumber: it.AssetNumber, Name: it.Name, Spec: it.Spec,
			DeviceUdid:   it.DeviceUdid,
			SerialNumber: it.SerialNumber, Model: it.Model, OSVersion: it.OSVersion,
			AssetStatus: it.AssetStatus, CategoryID: it.CategoryID, CategoryName: it.CategoryName,
			LastReturnLocation: it.LastReturnLocation,
		}
		if it.LastReturnAt != nil {
			s := it.LastReturnAt.Format(time.RFC3339)
			rr.LastReturnAt = &s
		}
		rows = append(rows, rr)
	}
	writeJSON(w, map[string]interface{}{"assets": rows})
}

// handleRentals godoc
// @Summary 借用單列表 / 建立借用單
// @Tags Rental
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param status query string false "狀態篩選" Enums(pending,approved,active,returned,rejected)
// @Param device_udid query string false "裝置 UDID"
// @Param show_archived query string false "顯示已歸檔" Enums(true,false)
// @Param body body swagRentalReq false "建立借用單（POST）"
// @Success 200 {object} map[string]interface{} "GET: {rentals: [...]}, POST: {ids, count, rental_number}"
// @Failure 409 {object} swagError "裝置不可用"
// @Router /api/rentals [get]
// @Router /api/rentals [post]
func (c *RentalController) handleRentals(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireModule(r, "rental", "requester")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		status := r.URL.Query().Get("status")
		deviceUdid := r.URL.Query().Get("device_udid")
		showArchived := r.URL.Query().Get("show_archived") == "true"

		rentals, err := c.rentalRepo.List(r.Context(), status, deviceUdid, showArchived)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Fetched once and reused for every row's daily-tracking resolution
		// below, rather than re-querying the (small) category tree per row.
		categoriesCache, _ := c.categoryRepo.List(r.Context())
		rows := make([]map[string]interface{}, 0, len(rentals))
		for _, rl := range rentals {
			row := map[string]interface{}{
				"id": rl.ID, "asset_id": rl.AssetID, "device_udid": rl.DeviceUdid,
				"borrower_id": rl.BorrowerID, "borrower_name": rl.BorrowerName,
				"approver_id": rl.ApproverID, "approver_name": rl.ApproverName,
				"status": rl.Status, "purpose": rl.Purpose,
				"borrow_date": rl.BorrowDate.Format(time.RFC3339), "notes": rl.Notes,
				"created_at": rl.CreatedAt.Format(time.RFC3339), "updated_at": rl.UpdatedAt.Format(time.RFC3339),
				"device_name": rl.DeviceName, "device_serial": rl.DeviceSerial,
				"asset_number": rl.AssetNumber, "asset_name": rl.AssetName,
				"custodian_id": rl.CustodianID, "custodian_name": rl.CustodianName,
				"rental_number": rl.RentalNumber, "is_archived": rl.IsArchived,
				"return_checklist": rl.ReturnChecklist, "return_notes": rl.ReturnNotes,
				"multi_day_reason": rl.MultiDayReason, "category_id": rl.CategoryID,
				"return_checklist_reported": rl.ReturnChecklistReported,
				"return_reported_by":        rl.ReturnReportedBy,
				"cross_day_reason":          rl.CrossDayReason,
			}
			if rl.ReturnReportedAt != nil {
				row["return_reported_at"] = rl.ReturnReportedAt.Format(time.RFC3339)
			} else {
				row["return_reported_at"] = nil
			}
			if rl.CategoryID != nil {
				dailyTracking, dErr := c.rentalRuleRepo.ResolveDailyTrackingRequired(r.Context(), categoriesCache, *rl.CategoryID)
				row["daily_tracking_required"] = dErr == nil && dailyTracking
			} else {
				row["daily_tracking_required"] = false
			}
			if rl.ExpectedReturn != nil {
				row["expected_return"] = rl.ExpectedReturn.Format("2006-01-02")
			} else {
				row["expected_return"] = nil
			}
			if rl.ActualReturn != nil {
				row["actual_return"] = rl.ActualReturn.Format(time.RFC3339)
			} else {
				row["actual_return"] = nil
			}
			rows = append(rows, row)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"rentals": rows})

	case http.MethodPost:
		var body struct {
			AssetIDs       []string `json:"asset_ids"`
			DeviceUdids    []string `json:"device_udids"` // legacy fallback
			BorrowerID     string   `json:"borrower_id"`
			Purpose        string   `json:"purpose"`
			BorrowDate     *string  `json:"borrow_date"`
			ExpectedReturn *string  `json:"expected_return"`
			Notes          string   `json:"notes"`
			// MultiDayReason is required when the booking spans more than one
			// calendar day AND at least one asset's category is flagged
			// daily_tracking_required (e.g. a multi-day vehicle trip).
			MultiDayReason string `json:"multi_day_reason"`
			// Category mode: multiple {category_id, quantity} lines.
			CategoryLines []struct {
				CategoryID string `json:"category_id"`
				Quantity   int    `json:"quantity"`
			} `json:"category_lines"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.BorrowerID == "" {
			writeError(w, http.StatusBadRequest, "asset_ids and borrower_id required")
			return
		}

		// Category mode: auto-pick available assets for each line.
		if len(body.AssetIDs) == 0 && len(body.CategoryLines) > 0 {
			for _, line := range body.CategoryLines {
				if line.CategoryID == "" || line.Quantity <= 0 {
					continue
				}
				ids, err := c.assetRepo.ListAvailableByCategory(r.Context(), line.CategoryID, line.Quantity)
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				if len(ids) < line.Quantity {
					writeError(w, http.StatusConflict, fmt.Sprintf("分類 %s 可用數量不足，需要 %d 筆，實際可用 %d 筆", line.CategoryID, line.Quantity, len(ids)))
					return
				}
				body.AssetIDs = append(body.AssetIDs, ids...)
			}
		}

		// Legacy: translate device_udids -> asset_ids via assets table.
		if len(body.AssetIDs) == 0 && len(body.DeviceUdids) > 0 {
			for _, udid := range body.DeviceUdids {
				a, err := c.assetRepo.GetByDeviceUdid(r.Context(), udid)
				if err != nil || a == nil {
					writeError(w, http.StatusBadRequest, "device has no linked asset: "+udid)
					return
				}
				body.AssetIDs = append(body.AssetIDs, a.ID)
			}
		}

		if len(body.AssetIDs) == 0 {
			writeError(w, http.StatusBadRequest, "asset_ids required")
			return
		}

		// Get borrower name
		borrower, err := c.userRepo.GetByID(r.Context(), body.BorrowerID)
		borrowerName := ""
		if err == nil {
			borrowerName = borrower.DisplayName
			if borrowerName == "" {
				borrowerName = borrower.Username
			}
		}

		// Check availability and collect asset → udid mapping for later use
		type resolvedAsset struct {
			assetID    string
			udid       string // may be empty for standalone
			name       string
			categoryID *string
		}
		resolved := make([]resolvedAsset, 0, len(body.AssetIDs))
		var unavailable []string
		for _, aid := range body.AssetIDs {
			a, err := c.assetRepo.GetByID(r.Context(), aid)
			if err != nil || a == nil {
				unavailable = append(unavailable, aid+" (不存在)")
				continue
			}
			status, isRented, name, _ := c.assetRepo.CheckAssetAvailability(r.Context(), aid)
			label := name
			if label == "" {
				label = a.AssetNumber
			}
			if isRented {
				unavailable = append(unavailable, label+" (借出中)")
				continue
			}
			if status != "available" && status != "" {
				unavailable = append(unavailable, label+" ("+status+")")
				continue
			}
			udid := ""
			if a.DeviceUdid != nil {
				udid = *a.DeviceUdid
			}
			resolved = append(resolved, resolvedAsset{assetID: aid, udid: udid, name: label, categoryID: a.CategoryID})
		}
		if len(unavailable) > 0 {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "部分資產目前無法借出",
				"devices": unavailable,
			})
			return
		}

		rentalNumber, _ := c.rentalRepo.NextRentalNumber(r.Context())

		// Phase 1 of 租借 2.1: borrow_date is fillable at creation (unrestricted
		// range), defaulting to today when omitted — same as the old DB
		// DEFAULT now() behavior, just made explicit so it can also be
		// compared against expected_return below.
		borrowDate := time.Now()
		if body.BorrowDate != nil && strings.TrimSpace(*body.BorrowDate) != "" {
			t, err := time.Parse("2006-01-02", *body.BorrowDate)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid borrow_date")
				return
			}
			borrowDate = t
		}

		var expectedReturn *time.Time
		if body.ExpectedReturn != nil && *body.ExpectedReturn != "" {
			t, err := time.Parse("2006-01-02", *body.ExpectedReturn)
			if err == nil {
				expectedReturn = &t
			}
		}

		// Vehicle "daily tracking" rule: a booking spanning more than one
		// calendar day, where any involved asset's category resolves to
		// daily_tracking_required, must explain why.
		categoryIDs := make([]*string, 0, len(resolved))
		for _, it := range resolved {
			categoryIDs = append(categoryIDs, it.categoryID)
		}
		dailyTrackingRequired, err := c.resolveDailyTrackingRequired(r.Context(), categoryIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		isMultiDay := expectedReturn != nil && !sameCalendarDay(borrowDate, *expectedReturn)
		multiDayReason := strings.TrimSpace(body.MultiDayReason)
		if dailyTrackingRequired && isMultiDay && multiDayReason == "" {
			writeError(w, http.StatusBadRequest, "此租借涉及逐日追蹤分類（如車輛），跨日租借需填寫說明")
			return
		}

		var ids []string
		for _, it := range resolved {
			assetID := it.assetID
			rental := &domain.Rental{
				AssetID:        &assetID,
				DeviceUdid:     it.udid,
				BorrowerID:     body.BorrowerID,
				BorrowerName:   borrowerName,
				Purpose:        body.Purpose,
				BorrowDate:     borrowDate,
				MultiDayReason: multiDayReason,
				ExpectedReturn: expectedReturn,
				Notes:          body.Notes,
				RentalNumber:   rentalNumber,
			}
			id, err := c.rentalRepo.Create(r.Context(), rental)
			if err != nil {
				log.Printf("rental insert: %v", err)
				continue
			}
			ids = append(ids, id)
		}
		// Notify custodians/approvers about the new rental request
		go func() {
			bgCtx := context.Background()
			data := c.buildNotifyData(bgCtx, rentalNumber, borrowerName, "", body.Purpose, body.Notes, expectedReturn)
			notified := map[string]bool{}
			for _, it := range resolved {
				a, err := c.assetRepo.GetByID(bgCtx, it.assetID)
				if err != nil || a == nil || a.CustodianID == nil || *a.CustodianID == "" {
					continue
				}
				custodian, err := c.userRepo.GetByID(bgCtx, *a.CustodianID)
				if err == nil && custodian.Email != "" && !notified[custodian.Email] {
					c.notifySvc.SendRentalRequest(bgCtx, data, custodian.Email)
					notified[custodian.Email] = true
				}
			}
		}()
		_ = claims
		json.NewEncoder(w).Encode(map[string]interface{}{"ids": ids, "count": len(ids), "rental_number": rentalNumber})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleRentalByID godoc
// @Summary 借用單操作：approve / activate / submit-return / return / reject / delete
// @Tags Rental
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "借用單 ID"
// @Param action path string false "操作" Enums(approve,activate,submit-return,return,reject,daily-report)
// @Param body body swagReturnReq false "歸還核對資訊（return 時使用，見 swagSubmitReturnReq 為 submit-return 時使用）"
// @Success 200 {object} swagOK
// @Failure 400 {object} swagError
// @Failure 404 {object} swagError
// @Router /api/rentals/{id}/{action} [post]
// @Router /api/rentals/{id} [delete]
func (c *RentalController) handleRentalByID(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireModule(r, "rental", "requester")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/rentals/"), "/")
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	if r.Method == http.MethodPost && action != "" {
		// Get approver display name
		approver, _ := c.userRepo.GetByID(r.Context(), claims.UserID)
		approverDisplayName := claims.Username
		if approver != nil {
			if approver.DisplayName != "" {
				approverDisplayName = approver.DisplayName
			}
		}

		rental, err := c.rentalRepo.GetByID(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "rental not found")
			return
		}

		switch action {
		case "approve":
			if rental.Status != "pending" {
				writeError(w, http.StatusBadRequest, "rental is not pending")
				return
			}
			c.rentalRepo.UpdateStatusByNumber(r.Context(), rental.RentalNumber, "pending", "approved", &claims.UserID, approverDisplayName)
			// Notify borrower that rental is approved
			go func() {
				bgCtx := context.Background()
				borrowerID, borrowerName, _ := c.rentalRepo.GetBorrowerInfo(bgCtx, id)
				data := c.buildNotifyData(bgCtx, rental.RentalNumber, borrowerName, approverDisplayName, "", "", nil)
				borrower, err := c.userRepo.GetByID(bgCtx, borrowerID)
				if err == nil && borrower.Email != "" {
					c.notifySvc.SendRentalApproved(bgCtx, data, borrower.Email)
				}
			}()
			writeJSON(w, map[string]interface{}{"ok": true, "status": "approved"})

		case "activate":
			if rental.Status != "approved" {
				writeError(w, http.StatusBadRequest, "rental is not approved")
				return
			}
			c.rentalRepo.ActivateByNumber(r.Context(), rental.RentalNumber)
			borrowerID, borrowerName, _ := c.rentalRepo.GetBorrowerInfo(r.Context(), id)
			assetIDs, _ := c.rentalRepo.ListAssetIDsByNumber(r.Context(), rental.RentalNumber)
			for _, aid := range assetIDs {
				c.assetRepo.SetHolderByID(r.Context(), aid, borrowerID, borrowerName)
			}
			// Notify borrower that devices are handed out
			go func() {
				bgCtx := context.Background()
				data := c.buildNotifyData(bgCtx, rental.RentalNumber, borrowerName, approverDisplayName, "", "", nil)
				borrower, err := c.userRepo.GetByID(bgCtx, borrowerID)
				if err == nil && borrower.Email != "" {
					c.notifySvc.SendRentalActivated(bgCtx, data, borrower.Email)
				}
			}()
			log.Printf("[rental] batch activated: rental_number=%d borrower=%s", rental.RentalNumber, borrowerName)
			writeJSON(w, map[string]interface{}{"ok": true, "status": "active"})

		case "submit-return":
			if rental.Status != "active" {
				writeError(w, http.StatusBadRequest, "rental is not active")
				return
			}
			// Stage 1: the borrower (or an admin on their behalf) reports the
			// checklist while the devices are still in their hands.
			borrowerID, borrowerName, err := c.rentalRepo.GetBorrowerInfo(r.Context(), rental.ID)
			if err != nil {
				writeError(w, http.StatusNotFound, "rental not found")
				return
			}
			if claims.UserID != borrowerID && claims.Role != "admin" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			var submitBody struct {
				Notes     string                 `json:"notes"`
				Checklist map[string]interface{} `json:"checklist"`
			}
			json.NewDecoder(r.Body).Decode(&submitBody)
			var reportedJSON []byte
			if submitBody.Checklist != nil {
				reportedJSON, _ = json.Marshal(submitBody.Checklist)
			}
			if err := c.rentalRepo.SubmitReturnByNumber(r.Context(), rental.RentalNumber, reportedJSON, claims.UserID); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			// If this rental is under daily tracking, today's submit-return
			// also counts as today's daily report — otherwise the day the
			// devices actually come back would be the one day with no entry.
			if required, _ := c.resolveDailyTrackingRequiredForRentalNumber(r.Context(), rental.RentalNumber); required {
				today := dateOnly(time.Now())
				if exists, _ := c.dailyReportRepo.ExistsForDate(r.Context(), rental.RentalNumber, today); !exists {
					reportedBy := claims.UserID
					c.dailyReportRepo.Create(r.Context(), &domain.RentalDailyReport{
						RentalNumber: rental.RentalNumber, ReportDate: today,
						Checklist: submitBody.Checklist, ReportedBy: &reportedBy,
					})
				}
			}
			// Notify custodian(s) there's a return pending their verification.
			go func() {
				bgCtx := context.Background()
				data := c.buildNotifyData(bgCtx, rental.RentalNumber, borrowerName, "", "", submitBody.Notes, nil)
				assetIDs, _ := c.rentalRepo.ListAssetIDsByNumber(bgCtx, rental.RentalNumber)
				notified := map[string]bool{}
				for _, aid := range assetIDs {
					a, err := c.assetRepo.GetByID(bgCtx, aid)
					if err != nil || a == nil || a.CustodianID == nil || *a.CustodianID == "" {
						continue
					}
					custodian, err := c.userRepo.GetByID(bgCtx, *a.CustodianID)
					if err == nil && custodian.Email != "" && !notified[custodian.Email] {
						c.notifySvc.SendRentalPendingVerification(bgCtx, data, custodian.Email)
						notified[custodian.Email] = true
					}
				}
			}()
			log.Printf("[rental] batch return reported: rental_number=%d reporter=%s", rental.RentalNumber, claims.Username)
			writeJSON(w, map[string]interface{}{"ok": true, "status": "pending_return"})

		case "return":
			if rental.Status != "pending_return" {
				writeError(w, http.StatusBadRequest, "rental is not pending return")
				return
			}
			var returnBody struct {
				Notes          string                 `json:"notes"`
				Checklist      map[string]interface{} `json:"checklist"`
				CrossDayReason string                 `json:"cross_day_reason"`
			}
			json.NewDecoder(r.Body).Decode(&returnBody)

			// 逐日追蹤分類：實際歸還（今天）晚於預計歸還日期時，不硬擋，但強制
			// 要求填寫逾期原因，供事後查閱。
			if required, _ := c.resolveDailyTrackingRequiredForRentalNumber(r.Context(), rental.RentalNumber); required {
				if rental.ExpectedReturn != nil && dateOnly(time.Now()).After(dateOnly(*rental.ExpectedReturn)) && strings.TrimSpace(returnBody.CrossDayReason) == "" {
					writeError(w, http.StatusBadRequest, "已超過預計歸還日期，需填寫逾期原因")
					return
				}
			}

			var checklistJSON []byte
			if returnBody.Checklist != nil {
				checklistJSON, _ = json.Marshal(returnBody.Checklist)
			}
			c.rentalRepo.ReturnByNumber(r.Context(), rental.RentalNumber, checklistJSON, returnBody.Notes, claims.UserID, strings.TrimSpace(returnBody.CrossDayReason))
			assetIDs, _ := c.rentalRepo.ListAssetIDsByNumber(r.Context(), rental.RentalNumber)
			for _, aid := range assetIDs {
				c.assetRepo.ClearHolderByID(r.Context(), aid)
			}
			// Phase 2c: if the verified checklist answered a "location" type
			// item, record it on every asset in this batch as their last
			// known return location (AssetPicker surfaces it for the next
			// person choosing this asset). Resolved from the categories
			// actually involved, not guessed from key names.
			if returnBody.Checklist != nil {
				categoryIDs := make([]*string, 0, len(assetIDs))
				for _, aid := range assetIDs {
					a, err := c.assetRepo.GetByID(r.Context(), aid)
					if err == nil && a != nil {
						categoryIDs = append(categoryIDs, a.CategoryID)
					}
				}
				if cats, err := c.categoryRepo.List(r.Context()); err == nil {
					var catIDStrs []string
					for _, cid := range categoryIDs {
						if cid != nil && *cid != "" {
							catIDStrs = append(catIDStrs, *cid)
						}
					}
					if items, err := c.templateRepo.ResolveMerged(r.Context(), cats, catIDStrs); err == nil {
						for _, it := range items {
							if it.Type != "location" {
								continue
							}
							v, ok := returnBody.Checklist[it.Key]
							if !ok || v == nil {
								continue
							}
							locJSON, err := json.Marshal(v)
							if err != nil {
								continue
							}
							for _, aid := range assetIDs {
								c.assetRepo.UpdateLastReturnLocation(r.Context(), aid, locJSON)
							}
							break
						}
					}
				}
			}
			// Notify custodian that devices are returned
			go func() {
				bgCtx := context.Background()
				data := c.buildNotifyData(bgCtx, rental.RentalNumber, "", approverDisplayName, "", returnBody.Notes, nil)
				data.ReturnNotes = returnBody.Notes
				// Notify each custodian
				notified := map[string]bool{}
				for _, aid := range assetIDs {
					a, err := c.assetRepo.GetByID(bgCtx, aid)
					if err != nil || a == nil || a.CustodianID == nil || *a.CustodianID == "" {
						continue
					}
					custodian, err := c.userRepo.GetByID(bgCtx, *a.CustodianID)
					if err == nil && custodian.Email != "" && !notified[custodian.Email] {
						c.notifySvc.SendRentalReturned(bgCtx, data, custodian.Email)
						notified[custodian.Email] = true
					}
				}
			}()
			log.Printf("[rental] batch returned: rental_number=%d holder cleared", rental.RentalNumber)
			writeJSON(w, map[string]interface{}{"ok": true, "status": "returned"})

		case "reject":
			if rental.Status != "pending" {
				writeError(w, http.StatusBadRequest, "rental is not pending")
				return
			}
			c.rentalRepo.UpdateStatusByNumber(r.Context(), rental.RentalNumber, "pending", "rejected", &claims.UserID, approverDisplayName)
			// Notify borrower that rental is rejected
			go func() {
				bgCtx := context.Background()
				borrowerID, borrowerName, _ := c.rentalRepo.GetBorrowerInfo(bgCtx, id)
				data := c.buildNotifyData(bgCtx, rental.RentalNumber, borrowerName, approverDisplayName, "", "", nil)
				borrower, err := c.userRepo.GetByID(bgCtx, borrowerID)
				if err == nil && borrower.Email != "" {
					c.notifySvc.SendRentalRejected(bgCtx, data, borrower.Email)
				}
			}()
			writeJSON(w, map[string]interface{}{"ok": true, "status": "rejected"})

		case "daily-report":
			c.handleDailyReport(w, r, claims, rental)

		default:
			w.WriteHeader(http.StatusBadRequest)
		}
		return
	}

	if r.Method == http.MethodGet && action == "daily-reports" {
		rental, err := c.rentalRepo.GetByID(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "rental not found")
			return
		}
		reports, err := c.dailyReportRepo.ListByNumber(r.Context(), rental.RentalNumber)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rows := make([]map[string]interface{}, 0, len(reports))
		for _, rep := range reports {
			rows = append(rows, map[string]interface{}{
				"id":              rep.ID,
				"report_date":     rep.ReportDate.Format("2006-01-02"),
				"checklist":       rep.Checklist,
				"backfill_reason": rep.BackfillReason,
				"reported_by":     rep.ReportedBy,
				"reported_at":     rep.ReportedAt.Format(time.RFC3339),
			})
		}
		writeJSON(w, map[string]interface{}{"reports": rows})
		return
	}

	if r.Method == http.MethodDelete {
		rental, err := c.rentalRepo.GetByID(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "rental not found")
			return
		}
		c.rentalRepo.DeleteByNumber(r.Context(), rental.RentalNumber)
		writeOK(w)
		return
	}

	w.WriteHeader(http.StatusMethodNotAllowed)
}

// handleDailyReport godoc
// @Summary 逐日追蹤分類的每日回報（跟歸還是分開的動作，裝置狀態不變）
// @Tags Rental
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "借用單 ID"
// @Success 200 {object} swagOK
// @Failure 400 {object} swagError
// @Failure 403 {object} swagError
// @Failure 404 {object} swagError
// @Failure 409 {object} swagError
// @Router /api/rentals/{id}/daily-report [post]
func (c *RentalController) handleDailyReport(w http.ResponseWriter, r *http.Request, claims *middleware.Claims, rental *domain.Rental) {
	if rental.Status != "active" {
		writeError(w, http.StatusBadRequest, "rental is not active")
		return
	}

	borrowerID, _, err := c.rentalRepo.GetBorrowerInfo(r.Context(), rental.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "rental not found")
		return
	}
	if claims.UserID != borrowerID && claims.Role != "admin" {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	required, err := c.resolveDailyTrackingRequiredForRentalNumber(r.Context(), rental.RentalNumber)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !required {
		// This rental's category isn't flagged daily_tracking_required —
		// daily reports don't apply to it, so this endpoint simply doesn't
		// exist for it.
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var body struct {
		Checklist      map[string]interface{} `json:"checklist"`
		ReportDate     *string                `json:"report_date"`
		BackfillReason string                 `json:"backfill_reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	today := dateOnly(time.Now())
	reportDate := today
	backfillReason := ""
	if body.ReportDate != nil && strings.TrimSpace(*body.ReportDate) != "" {
		parsed, err := time.Parse("2006-01-02", *body.ReportDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid report_date")
			return
		}
		parsed = dateOnly(parsed)
		if !parsed.Before(today) {
			writeError(w, http.StatusBadRequest, "report_date 必須是過去日期（今天請不要帶 report_date）")
			return
		}
		if parsed.Before(dateOnly(rental.BorrowDate)) {
			writeError(w, http.StatusBadRequest, "report_date 早於借出日期")
			return
		}
		if rental.ExpectedReturn != nil && parsed.After(dateOnly(*rental.ExpectedReturn)) {
			writeError(w, http.StatusBadRequest, "report_date 超出租期範圍")
			return
		}
		backfillReason = strings.TrimSpace(body.BackfillReason)
		if backfillReason == "" {
			writeError(w, http.StatusBadRequest, "補登過去日期需填寫原因")
			return
		}
		reportDate = parsed
	}

	exists, err := c.dailyReportRepo.ExistsForDate(r.Context(), rental.RentalNumber, reportDate)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if exists {
		writeError(w, http.StatusConflict, "這天已經回報過了")
		return
	}

	reportedBy := claims.UserID
	if _, err := c.dailyReportRepo.Create(r.Context(), &domain.RentalDailyReport{
		RentalNumber:   rental.RentalNumber,
		ReportDate:     reportDate,
		Checklist:      body.Checklist,
		BackfillReason: backfillReason,
		ReportedBy:     &reportedBy,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "report_date": reportDate.Format("2006-01-02"), "backfilled": backfillReason != ""})
}

// handleExport godoc
// @Summary 匯出借用記錄為 Excel
// @Tags Rental
// @Produce application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Security BearerAuth
// @Param status query string false "狀態篩選"
// @Param ids query string false "僅匯出指定借用單 ID（逗號分隔）"
// @Success 200 {file} file "Excel 檔案"
// @Router /api/rentals-export [get]
func (c *RentalController) handleExport(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireModule(r, "rental", "requester"); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	status := r.URL.Query().Get("status")
	rentals, err := c.rentalRepo.List(r.Context(), status, "", false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// If specific IDs provided, filter
	if idsParam := r.URL.Query().Get("ids"); idsParam != "" {
		idSet := map[string]bool{}
		for _, id := range strings.Split(idsParam, ",") {
			idSet[strings.TrimSpace(id)] = true
		}
		filtered := make([]*domain.Rental, 0)
		for _, rl := range rentals {
			if idSet[rl.ID] {
				filtered = append(filtered, rl)
			}
		}
		rentals = filtered
	}

	// Group by rental_number
	type rentalGroup struct {
		Number  int
		Rentals []*domain.Rental
		First   *domain.Rental
	}
	groupMap := map[int]*rentalGroup{}
	for _, rl := range rentals {
		g, ok := groupMap[rl.RentalNumber]
		if !ok {
			g = &rentalGroup{Number: rl.RentalNumber, First: rl}
			groupMap[rl.RentalNumber] = g
		}
		g.Rentals = append(g.Rentals, rl)
	}
	groups := make([]*rentalGroup, 0, len(groupMap))
	for _, g := range groupMap {
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Number > groups[j].Number })

	statusLabels := map[string]string{
		"pending": "待核准", "approved": "已核准", "active": "借出中",
		"returned": "已歸還", "rejected": "已拒絕",
	}

	// Dynamic checklist columns (Phase 2a): scan the keys that actually
	// appear in this export's return_checklist data, in first-seen order,
	// rather than a hardcoded 5-column list. Also try to resolve each key's
	// real label/type from the checklist templates of categories represented
	// in this export, falling back to the bare key when a key isn't found in
	// any of them (e.g. old data from a since-edited template).
	var checklistKeys []string
	seenKeys := map[string]bool{}
	for _, g := range groups {
		for k := range g.First.ReturnChecklist {
			if !seenKeys[k] {
				seenKeys[k] = true
				checklistKeys = append(checklistKeys, k)
			}
		}
	}
	itemMeta := map[string]domain.ChecklistItem{}
	if len(checklistKeys) > 0 {
		catsCache, _ := c.categoryRepo.List(r.Context())
		seenCategories := map[string]bool{}
		for _, g := range groups {
			for _, rl := range g.Rentals {
				if rl.CategoryID == nil || seenCategories[*rl.CategoryID] {
					continue
				}
				seenCategories[*rl.CategoryID] = true
				items, err := c.templateRepo.ResolveForCategory(r.Context(), catsCache, *rl.CategoryID)
				if err != nil {
					continue
				}
				for _, it := range items {
					if _, ok := itemMeta[it.Key]; !ok {
						itemMeta[it.Key] = it
					}
				}
			}
		}
	}

	f := excelize.NewFile()
	sheet := "租借記錄"
	f.SetSheetName("Sheet1", sheet)

	// Headers
	headers := []string{
		"單號", "裝置數", "裝置名稱", "裝置序號", "借用人", "保管人",
		"用途", "狀態", "借出日期", "預計歸還", "實際歸還", "核准人", "備註",
	}
	for _, k := range checklistKeys {
		label := k
		if it, ok := itemMeta[k]; ok && it.Label != "" {
			label = it.Label
		}
		headers = append(headers, "歸還清點-"+label)
	}
	headers = append(headers, "歸還備註", "存查")

	for col, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		f.SetCellValue(sheet, cell, h)
	}

	// Bold header style
	style, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	endCell, _ := excelize.CoordinatesToCellName(len(headers), 1)
	f.SetCellStyle(sheet, "A1", endCell, style)

	// Data rows
	for row, g := range groups {
		r := row + 2 // 1-indexed, skip header
		rl := g.First

		deviceNames := make([]string, 0, len(g.Rentals))
		deviceSerials := make([]string, 0, len(g.Rentals))
		for _, item := range g.Rentals {
			name := item.DeviceName
			if name == "" {
				name = item.DeviceSerial
			}
			deviceNames = append(deviceNames, name)
			deviceSerials = append(deviceSerials, item.DeviceSerial)
		}

		statusLabel := statusLabels[rl.Status]
		if statusLabel == "" {
			statusLabel = rl.Status
		}

		borrowDate := ""
		if !rl.BorrowDate.IsZero() {
			borrowDate = rl.BorrowDate.Format("2006-01-02")
		}
		expectedReturn := ""
		if rl.ExpectedReturn != nil {
			expectedReturn = rl.ExpectedReturn.Format("2006-01-02")
		}
		actualReturn := ""
		if rl.ActualReturn != nil {
			actualReturn = rl.ActualReturn.Format("2006-01-02")
		}

		vals := []interface{}{
			g.Number,
			len(g.Rentals),
			strings.Join(deviceNames, "、"),
			strings.Join(deviceSerials, "、"),
			rl.BorrowerName,
			rl.CustodianName,
			rl.Purpose,
			statusLabel,
			borrowDate,
			expectedReturn,
			actualReturn,
			rl.ApproverName,
			rl.Notes,
		}

		// Checklist columns — rendered per the item's resolved type where
		// known (boolean → "V" when true, number appends its unit, location
		// prints an address or "lat,lng", photo prints an item count),
		// falling back to a plain string print for anything else/unresolved.
		cl := rl.ReturnChecklist
		for _, k := range checklistKeys {
			vals = append(vals, formatChecklistValue(itemMeta[k], cl[k]))
		}

		// Return notes + archived
		vals = append(vals, rl.ReturnNotes)
		archived := ""
		if rl.IsArchived {
			archived = "是"
		}
		vals = append(vals, archived)

		for col, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(col+1, r)
			f.SetCellValue(sheet, cell, v)
		}
	}

	// Auto-fit column widths (approximate)
	for col := range headers {
		colName, _ := excelize.ColumnNumberToName(col + 1)
		f.SetColWidth(sheet, colName, colName, 14)
	}

	now := time.Now().Format("2006-01-02")
	filename := fmt.Sprintf("租借記錄_%s.xlsx", now)

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	if err := f.Write(w); err != nil {
		log.Printf("[rental-export] write error: %v", err)
	}
	f.Close()
}

// handleArchive godoc
// @Summary 歸檔借用單
// @Tags Rental
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body swagArchiveReq true "借用單 ID 列表"
// @Success 200 {object} swagOK
// @Failure 400 {object} swagError
// @Router /api/rentals-archive [post]
func (c *RentalController) handleArchive(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireModule(r, "rental", "manager"); err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids required")
		return
	}
	if err := c.rentalRepo.Archive(r.Context(), body.IDs); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w)
}
