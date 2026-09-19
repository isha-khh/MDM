package controller

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/anthropics/mdm-server/internal/adapter/postgres"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/port"
)

// CategoryNoticeController serves the rental "please read and agree" notice
// flow: resolving which notices (if any) the current user still needs to
// agree to for a given set of asset categories, and recording their
// agreement. Per-category notice maintenance lives in CategoryController's
// /notice sub-route instead.
type CategoryNoticeController struct {
	categoryRepo port.CategoryRepository
	noticeRepo   *postgres.CategoryNoticeRepo
	auth         *middleware.AuthHelper
}

func NewCategoryNoticeController(categoryRepo port.CategoryRepository, noticeRepo *postgres.CategoryNoticeRepo, auth *middleware.AuthHelper) *CategoryNoticeController {
	return &CategoryNoticeController{categoryRepo: categoryRepo, noticeRepo: noticeRepo, auth: auth}
}

func (c *CategoryNoticeController) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/category-notices/resolve", c.handleResolve)
	mux.HandleFunc("/api/category-notice-acks", c.handleAck)
}

// handleResolve godoc
// @Summary 依分類清單解析目前使用者「尚需同意」的租借注意事項
// @Tags Rental
// @Produce json
// @Security BearerAuth
// @Param category_ids query string false "逗號分隔的分類 ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/category-notices/resolve [get]
func (c *CategoryNoticeController) handleResolve(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireModule(r, "rental", "requester")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	var categoryIDs []string
	for _, s := range strings.Split(r.URL.Query().Get("category_ids"), ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			categoryIDs = append(categoryIDs, s)
		}
	}

	cats, err := c.categoryRepo.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	pending, err := c.noticeRepo.ResolvePending(r.Context(), cats, categoryIDs, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"pending": pending})
}

// handleAck godoc
// @Summary 記錄目前使用者已同意指定分類的租借注意事項
// @Tags Rental
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} swagOK
// @Router /api/category-notice-acks [post]
func (c *CategoryNoticeController) handleAck(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireModule(r, "rental", "requester")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var body struct {
		CategoryIDs []string `json:"category_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	for _, cid := range body.CategoryIDs {
		if cid == "" {
			continue
		}
		if err := c.noticeRepo.Ack(r.Context(), cid, claims.UserID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeOK(w)
}
