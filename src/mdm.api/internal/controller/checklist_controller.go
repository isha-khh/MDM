package controller

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/anthropics/mdm-server/internal/adapter/postgres"
	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/port"
)

// ChecklistController serves the dynamic return-checklist system: resolving
// a merged template for a set of categories, uploading/downloading "photo"
// type answers, and maintaining the global default template (per-category
// templates are maintained via CategoryController's /checklist-template
// sub-route instead).
type ChecklistController struct {
	categoryRepo port.CategoryRepository
	templateRepo *postgres.ChecklistTemplateRepo
	photoRepo    *postgres.ChecklistPhotoRepo
	auth         *middleware.AuthHelper
}

func NewChecklistController(categoryRepo port.CategoryRepository, templateRepo *postgres.ChecklistTemplateRepo, photoRepo *postgres.ChecklistPhotoRepo, auth *middleware.AuthHelper) *ChecklistController {
	return &ChecklistController{categoryRepo: categoryRepo, templateRepo: templateRepo, photoRepo: photoRepo, auth: auth}
}

func (c *ChecklistController) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/checklist-templates/resolve", c.handleResolve)
	mux.HandleFunc("/api/checklist-templates/default", c.handleDefaultTemplate)
	mux.HandleFunc("/api/checklist-photos", c.handlePhotoUpload)
	mux.HandleFunc("/api/checklist-photos/", c.handlePhotoDownload)
}

// handleResolve godoc
// @Summary 依分類清單解析合併後的動態歸還檢查清單（含繼承、多分類聯集）
// @Tags Rental
// @Produce json
// @Security BearerAuth
// @Param category_ids query string false "逗號分隔的分類 ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/checklist-templates/resolve [get]
func (c *ChecklistController) handleResolve(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireModule(r, "rental", "requester"); err != nil {
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
	items, err := c.templateRepo.ResolveMerged(r.Context(), cats, categoryIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"items": items})
}

// handleDefaultTemplate godoc
// @Summary 取得/設定全域預設歸還檢查清單範本（分類沒有自己的設定時套用）
// @Tags Rental
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Router /api/checklist-templates/default [get]
// @Router /api/checklist-templates/default [put]
func (c *ChecklistController) handleDefaultTemplate(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, err := c.auth.RequireModule(r, "rental", "requester"); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		tpl, err := c.templateRepo.Get(r.Context(), nil)
		if err != nil {
			writeJSON(w, map[string]interface{}{"items": []domain.ChecklistItem{}})
			return
		}
		writeJSON(w, map[string]interface{}{"items": tpl.Items})

	case http.MethodPut:
		claims, err := c.auth.RequireModule(r, "asset", "operator")
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			Items []domain.ChecklistItem `json:"items"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := c.templateRepo.Upsert(r.Context(), nil, body.Items, claims.UserID); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeOK(w)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handlePhotoUpload godoc
// @Summary 上傳一張歸還清點照片（type:"photo" 項目用）
// @Tags Rental
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Success 200 {object} swagIDResponse
// @Router /api/checklist-photos [post]
func (c *ChecklistController) handlePhotoUpload(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireModule(r, "rental", "requester")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	if err := r.ParseMultipartForm(15 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid form")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file required")
		return
	}
	defer file.Close()

	rentalNumber, _ := strconv.Atoi(r.FormValue("rental_number"))
	itemKey := r.FormValue("item_key")
	if rentalNumber == 0 || itemKey == "" {
		writeError(w, http.StatusBadRequest, "rental_number and item_key required")
		return
	}

	content, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read failed")
		return
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	uploadedBy := claims.UserID
	id, err := c.photoRepo.Create(r.Context(), &domain.ChecklistPhoto{
		RentalNumber: rentalNumber,
		ItemKey:      itemKey,
		Content:      content,
		ContentType:  contentType,
		Size:         len(content),
		UploadedBy:   &uploadedBy,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"id": id})
}

// handlePhotoDownload godoc
// @Summary 下載一張歸還清點照片
// @Tags Rental
// @Produce image/jpeg
// @Security BearerAuth
// @Param id path string true "照片 ID"
// @Success 200 {file} file "圖片內容"
// @Router /api/checklist-photos/{id} [get]
func (c *ChecklistController) handlePhotoDownload(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireModule(r, "rental", "requester"); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/checklist-photos/")
	photo, err := c.photoRepo.Get(r.Context(), id)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", photo.ContentType)
	w.Write(photo.Content)
}
