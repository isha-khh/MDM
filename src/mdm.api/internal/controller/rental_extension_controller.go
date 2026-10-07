package controller

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/mdm-server/internal/adapter/postgres"
	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/middleware"
)

func extensionJSON(e *domain.RentalExtension) map[string]interface{} {
	row := map[string]interface{}{
		"id":                        e.ID,
		"rental_number":             e.RentalNumber,
		"requested_by":              e.RequestedBy,
		"requested_by_name":         e.RequestedByName,
		"previous_expected_return":  e.PreviousExpectedReturn.Format("2006-01-02"),
		"requested_expected_return": e.RequestedExpectedReturn.Format("2006-01-02"),
		"reason":                    e.Reason,
		"status":                    e.Status,
		"decision_note":             e.DecisionNote,
		"created_at":                e.CreatedAt.Format(time.RFC3339),
		"decided_at":                nil,
	}
	if e.DecidedAt != nil {
		row["decided_at"] = e.DecidedAt.Format(time.RFC3339)
	}
	return row
}

// batchCustodianIDs returns the distinct custodian user IDs of every asset in
// a rental batch.
func (c *RentalController) batchCustodianIDs(ctx context.Context, rentalNumber int) []string {
	assetIDs, _ := c.rentalRepo.ListAssetIDsByNumber(ctx, rentalNumber)
	seen := map[string]bool{}
	var ids []string
	for _, aid := range assetIDs {
		a, err := c.assetRepo.GetByID(ctx, aid)
		if err != nil || a == nil || a.CustodianID == nil || *a.CustodianID == "" || seen[*a.CustodianID] {
			continue
		}
		seen[*a.CustodianID] = true
		ids = append(ids, *a.CustodianID)
	}
	return ids
}

// allowed applies domain.CanPerformRentalAction for the current user against
// a rental batch. The custodian lookup only runs for non-admins, since admins
// pass every check that a custodian would. Enforced here on the server — the
// UI only hides buttons.
func (c *RentalController) allowed(ctx context.Context, claims *middleware.Claims, action domain.RentalAction, rentalNumber int) bool {
	isAdmin := claims.Role == "admin"
	isCustodian := false
	if !isAdmin {
		for _, id := range c.batchCustodianIDs(ctx, rentalNumber) {
			if id == claims.UserID {
				isCustodian = true
				break
			}
		}
	}
	return domain.CanPerformRentalAction(action, isAdmin, isCustodian)
}

// handleExtend godoc
// @Summary 申請續借（延長預計歸還日），需經保管人/管理員審核後才生效
// @Tags Rental
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "借用單 ID"
// @Param body body swagExtendReq true "新的預計歸還日與原因"
// @Success 200 {object} swagIDResponse
// @Failure 400 {object} swagError
// @Failure 403 {object} swagError
// @Failure 409 {object} swagError "已有審核中的續借申請"
// @Router /api/rentals/{id}/extend [post]
func (c *RentalController) handleExtend(w http.ResponseWriter, r *http.Request, claims *middleware.Claims, rental *domain.Rental) {
	borrowerID, borrowerName, err := c.rentalRepo.GetBorrowerInfo(r.Context(), rental.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "rental not found")
		return
	}
	// Same rule as submit-return: the borrower, or an admin on their behalf.
	if claims.UserID != borrowerID && claims.Role != "admin" {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	var body struct {
		NewExpectedReturn string `json:"new_expected_return"`
		Reason            string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	requested, err := time.Parse("2006-01-02", body.NewExpectedReturn)
	if err != nil {
		writeError(w, http.StatusBadRequest, "新的歸還日期格式錯誤")
		return
	}
	if err := domain.ValidateExtensionRequest(rental.Status, rental.ExpectedReturn, requested, time.Now(), body.Reason); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	reason := strings.TrimSpace(body.Reason)
	id, err := c.extensionRepo.Create(r.Context(), &domain.RentalExtension{
		RentalNumber:            rental.RentalNumber,
		RequestedBy:             claims.UserID,
		PreviousExpectedReturn:  *rental.ExpectedReturn,
		RequestedExpectedReturn: requested,
		Reason:                  reason,
	})
	if err != nil {
		if errors.Is(err, postgres.ErrExtensionPendingExists) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Tell the custodian(s) there's a request waiting for them.
	go func() {
		bgCtx := context.Background()
		data := c.buildNotifyData(bgCtx, rental.RentalNumber, borrowerName, "", "", "", rental.ExpectedReturn)
		data.NewExpectedReturn = requested.Format("2006-01-02")
		data.ExtensionReason = reason
		notified := map[string]bool{}
		for _, cid := range c.batchCustodianIDs(bgCtx, rental.RentalNumber) {
			custodian, err := c.userRepo.GetByID(bgCtx, cid)
			if err == nil && custodian.Email != "" && !notified[custodian.Email] {
				c.notifySvc.SendRentalExtensionRequest(bgCtx, data, custodian.Email)
				notified[custodian.Email] = true
			}
		}
	}()
	log.Printf("[rental] extension requested: rental_number=%d %s -> %s by=%s", rental.RentalNumber,
		rental.ExpectedReturn.Format("2006-01-02"), requested.Format("2006-01-02"), claims.UserID)
	writeJSON(w, map[string]interface{}{"ok": true, "id": id})
}

// handleExtensionDecision godoc
// @Summary 審核續借申請（核准後才更新預計歸還日）
// @Tags Rental
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "續借申請 ID"
// @Param body body swagExtensionDecisionReq false "審核備註"
// @Success 200 {object} swagOK
// @Failure 403 {object} swagError
// @Failure 404 {object} swagError
// @Failure 409 {object} swagError "申請已被處理過"
// @Router /api/rental-extensions/{id}/approve [post]
// @Router /api/rental-extensions/{id}/reject [post]
func (c *RentalController) handleExtensionDecision(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireModule(r, "rental", "requester")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/rental-extensions/"), "/")
	if len(parts) != 2 || (parts[1] != "approve" && parts[1] != "reject") {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	id, action := parts[0], parts[1]

	ext, err := c.extensionRepo.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "extension request not found")
		return
	}
	if !c.allowed(r.Context(), claims, domain.RentalDecideExtension, ext.RentalNumber) {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	var body struct {
		Note string `json:"note"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	note := strings.TrimSpace(body.Note)

	var decided *domain.RentalExtension
	if action == "approve" {
		decided, err = c.extensionRepo.Approve(r.Context(), id, claims.UserID, note)
	} else {
		decided, err = c.extensionRepo.Reject(r.Context(), id, claims.UserID, note)
	}
	if err != nil {
		switch {
		case errors.Is(err, postgres.ErrExtensionNotPending):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, domain.ErrExtensionNotActive):
			writeError(w, http.StatusBadRequest, "此租借已不是借出中，無法續借")
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	approverName := claims.Username
	if u, err := c.userRepo.GetByID(r.Context(), claims.UserID); err == nil && u.DisplayName != "" {
		approverName = u.DisplayName
	}
	go func() {
		bgCtx := context.Background()
		borrowerID, borrowerName, err := c.rentalRepo.GetBorrowerByNumber(bgCtx, decided.RentalNumber)
		if err != nil {
			return
		}
		borrower, err := c.userRepo.GetByID(bgCtx, borrowerID)
		if err != nil || borrower.Email == "" {
			return
		}
		prev := decided.PreviousExpectedReturn
		data := c.buildNotifyData(bgCtx, decided.RentalNumber, borrowerName, approverName, "", "", &prev)
		data.NewExpectedReturn = decided.RequestedExpectedReturn.Format("2006-01-02")
		data.DecisionNote = note
		if action == "approve" {
			c.notifySvc.SendRentalExtensionApproved(bgCtx, data, borrower.Email)
		} else {
			c.notifySvc.SendRentalExtensionRejected(bgCtx, data, borrower.Email)
		}
	}()
	log.Printf("[rental] extension %s: id=%s rental_number=%d by=%s", decided.Status, id, decided.RentalNumber, claims.UserID)
	writeJSON(w, map[string]interface{}{"ok": true, "status": decided.Status})
}
