package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/anthropics/mdm-server/internal/adapter/postgres"
	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/port"
)

type CategoryController struct {
	categoryRepo   port.CategoryRepository
	auth           *middleware.AuthHelper
	rentalRuleRepo *postgres.CategoryRentalRuleRepo
	templateRepo   *postgres.ChecklistTemplateRepo
	noticeRepo     *postgres.CategoryNoticeRepo
}

func NewCategoryController(categoryRepo port.CategoryRepository, auth *middleware.AuthHelper, rentalRuleRepo *postgres.CategoryRentalRuleRepo, templateRepo *postgres.ChecklistTemplateRepo, noticeRepo *postgres.CategoryNoticeRepo) *CategoryController {
	return &CategoryController{categoryRepo: categoryRepo, auth: auth, rentalRuleRepo: rentalRuleRepo, templateRepo: templateRepo, noticeRepo: noticeRepo}
}

func (c *CategoryController) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/categories", c.handleCategories)
	mux.HandleFunc("/api/categories/", c.handleCategoryByID)
	mux.HandleFunc("/api/category-rental-rules/resolve", c.handleResolveDailyTracking)
}

// handleCategories godoc
// @Summary 分類列表（樹狀排序）/ 新增分類
// @Tags Category
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body swagCategoryReq false "新增分類（POST）"
// @Success 200 {object} map[string][]swagCategoryItem "GET: {categories: [...]}, POST: {id: ...}"
// @Router /api/categories [get]
// @Router /api/categories [post]
func (c *CategoryController) handleCategories(w http.ResponseWriter, r *http.Request) {
	minLevel := "viewer"
	if r.Method == http.MethodPost {
		minLevel = "operator"
	}
	if _, err := c.auth.RequireModule(r, "asset", minLevel); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		allCats, err := c.categoryRepo.List(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Build tree-ordered flat list: parent then children recursively
		type catJSON struct {
			ID        string  `json:"id"`
			ParentID  *string `json:"parent_id"`
			Name      string  `json:"name"`
			Level     int     `json:"level"`
			SortOrder int     `json:"sort_order"`
		}
		var sorted []catJSON
		var walk func(parentID *string)
		walk = func(parentID *string) {
			for _, cat := range allCats {
				match := false
				if parentID == nil && cat.ParentID == nil {
					match = true
				} else if parentID == nil && cat.ParentID != nil && *cat.ParentID == "" {
					match = true
				} else if parentID != nil && cat.ParentID != nil && *cat.ParentID == *parentID {
					match = true
				}
				if match {
					sorted = append(sorted, catJSON{
						ID: cat.ID, ParentID: cat.ParentID,
						Name: cat.Name, Level: cat.Level, SortOrder: cat.SortOrder,
					})
					walk(&cat.ID)
				}
			}
		}
		walk(nil)
		if sorted == nil {
			sorted = []catJSON{}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"categories": sorted})

	case http.MethodPost:
		var body struct {
			ParentID *string `json:"parent_id"`
			Name     string  `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		level := 0
		if body.ParentID != nil && *body.ParentID != "" {
			if l, err := c.categoryRepo.GetLevel(r.Context(), *body.ParentID); err == nil {
				level = l + 1
			}
		}
		id, err := c.categoryRepo.Create(r.Context(), &domain.Category{
			ParentID: body.ParentID, Name: body.Name, Level: level,
		})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"id": id})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleCategoryByID godoc
// @Summary 更新 / 刪除分類
// @Tags Category
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "分類 ID"
// @Param body body swagCategoryUpdateReq false "更新名稱（PUT）"
// @Success 200 {object} swagOK
// @Router /api/categories/{id} [put]
// @Router /api/categories/{id} [delete]
func (c *CategoryController) handleCategoryByID(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireModule(r, "asset", "operator")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/categories/")
	parts := strings.Split(trimmed, "/")
	id := parts[0]
	w.Header().Set("Content-Type", "application/json")

	if len(parts) == 2 && parts[1] == "rental-rule" {
		c.handleCategoryRentalRule(w, r, claims, id)
		return
	}
	if len(parts) == 2 && parts[1] == "checklist-template" {
		c.handleCategoryChecklistTemplate(w, r, claims, id)
		return
	}
	if len(parts) == 2 && parts[1] == "notice" {
		c.handleCategoryNotice(w, r, claims, id)
		return
	}

	switch r.Method {
	case http.MethodPut:
		var body struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		c.categoryRepo.Update(r.Context(), id, body.Name)
		writeOK(w)
	case http.MethodDelete:
		c.categoryRepo.Delete(r.Context(), id)
		writeOK(w)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleResolveDailyTracking godoc
// @Summary 依分類清單解析是否套用「逐日追蹤」規則（含繼承；任一分類命中即為 true）
// @Tags Category
// @Produce json
// @Security BearerAuth
// @Param category_ids query string false "逗號分隔的分類 ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/category-rental-rules/resolve [get]
func (c *CategoryController) handleResolveDailyTracking(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireModule(r, "rental", "requester"); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var categoryIDs []string
	for _, s := range strings.Split(r.URL.Query().Get("category_ids"), ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			categoryIDs = append(categoryIDs, s)
		}
	}

	cats, err := c.categoryRepo.List(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	required := false
	for _, cid := range categoryIDs {
		ok, err := c.rentalRuleRepo.ResolveDailyTrackingRequired(r.Context(), cats, cid)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if ok {
			required = true
			break
		}
	}
	writeJSON(w, map[string]interface{}{"daily_tracking_required": required})
}

// handleCategoryRentalRule godoc
// @Summary 取得/設定分類的「逐日追蹤」租借規則
// @Tags Category
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "分類 ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/categories/{id}/rental-rule [get]
// @Router /api/categories/{id}/rental-rule [put]
func (c *CategoryController) handleCategoryRentalRule(w http.ResponseWriter, r *http.Request, claims *middleware.Claims, categoryID string) {
	switch r.Method {
	case http.MethodGet:
		rule, err := c.rentalRuleRepo.Get(r.Context(), categoryID)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			// No explicit row for this category — treat as "not required"
			// (this GET only reports THIS category's own setting, not the
			// inherited/resolved value, so the maintenance UI can show
			// "not set at this level" distinctly from an explicit false).
			writeJSON(w, map[string]interface{}{"category_id": categoryID, "daily_tracking_required": false, "is_explicit": false})
			return
		}
		writeJSON(w, map[string]interface{}{"category_id": categoryID, "daily_tracking_required": rule.DailyTrackingRequired, "is_explicit": true})

	case http.MethodPut:
		var body struct {
			DailyTrackingRequired bool `json:"daily_tracking_required"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := c.rentalRuleRepo.Upsert(r.Context(), categoryID, body.DailyTrackingRequired, claims.UserID); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeOK(w)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleCategoryChecklistTemplate godoc
// @Summary 取得/設定分類的歸還檢查清單範本
// @Tags Category
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "分類 ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/categories/{id}/checklist-template [get]
// @Router /api/categories/{id}/checklist-template [put]
func (c *CategoryController) handleCategoryChecklistTemplate(w http.ResponseWriter, r *http.Request, claims *middleware.Claims, categoryID string) {
	switch r.Method {
	case http.MethodGet:
		tpl, err := c.templateRepo.Get(r.Context(), &categoryID)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			// No explicit template at this category — report empty items and
			// is_explicit:false so the maintenance UI can show "inherits from
			// parent/default" rather than an empty saved template.
			writeJSON(w, map[string]interface{}{"category_id": categoryID, "items": []domain.ChecklistItem{}, "is_explicit": false})
			return
		}
		writeJSON(w, map[string]interface{}{"category_id": categoryID, "items": tpl.Items, "is_explicit": true})

	case http.MethodPut:
		var body struct {
			Items []domain.ChecklistItem `json:"items"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := c.templateRepo.Upsert(r.Context(), &categoryID, body.Items, claims.UserID); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeOK(w)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleCategoryNotice godoc
// @Summary 取得/設定分類的租借注意事項（提交申請前需同意）
// @Tags Category
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "分類 ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/categories/{id}/notice [get]
// @Router /api/categories/{id}/notice [put]
func (c *CategoryController) handleCategoryNotice(w http.ResponseWriter, r *http.Request, claims *middleware.Claims, categoryID string) {
	switch r.Method {
	case http.MethodGet:
		notice, err := c.noticeRepo.Get(r.Context(), categoryID)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			// No explicit notice at this category — report empty content and
			// is_explicit:false so the maintenance UI can show "inherits from
			// parent / no notice" rather than an empty saved notice.
			writeJSON(w, map[string]interface{}{"category_id": categoryID, "content": "", "is_explicit": false})
			return
		}
		writeJSON(w, map[string]interface{}{"category_id": categoryID, "content": notice.Content, "is_explicit": true})

	case http.MethodPut:
		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// An empty body clears this category's own override so it falls back
		// to whatever its ancestor chain resolves to (or nothing).
		var err error
		if strings.TrimSpace(body.Content) == "" {
			err = c.noticeRepo.Delete(r.Context(), categoryID)
		} else {
			err = c.noticeRepo.Upsert(r.Context(), categoryID, body.Content, claims.UserID)
		}
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeOK(w)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
