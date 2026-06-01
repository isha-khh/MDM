package controller

import (
	"encoding/json"
	"net/http"

	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/port"
	"github.com/anthropics/mdm-server/internal/service"
)

type AuthController struct {
	userRepo   port.UserRepository
	authHelper *middleware.AuthHelper
	jwtSecret  string
}

func NewAuthController(userRepo port.UserRepository, authHelper *middleware.AuthHelper, jwtSecret string) *AuthController {
	return &AuthController{userRepo: userRepo, authHelper: authHelper, jwtSecret: jwtSecret}
}

func (c *AuthController) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/login", c.handleLogin)
	mux.HandleFunc("/api/logout", c.handleLogout)
	mux.HandleFunc("/api/me", c.handleMe)
}

// isSecureRequest reports whether the request reached us over HTTPS, either
// directly (r.TLS) or via a reverse proxy that set X-Forwarded-Proto=https.
// We use this to set Cookie.Secure dynamically so dev (HTTP localhost) keeps
// working while production (mdm.isha.net behind TLS) gets a properly-protected
// cookie that browsers refuse to send over plain HTTP.
func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return r.Header.Get("X-Forwarded-Proto") == "https"
}

// handleLogin godoc
// @Summary 使用者登入
// @Tags Auth
// @Accept json
// @Produce json
// @Param body body swagLoginReq true "登入帳密"
// @Success 200 {object} swagLoginResp
// @Failure 401 {object} swagError
// @Failure 403 {object} swagError "帳號未啟用"
// @Router /api/login [post]
func (c *AuthController) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	user, err := c.userRepo.GetByUsername(r.Context(), body.Username)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if !service.VerifyPassword(user.PasswordHash, body.Password) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if !user.IsActive {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"account pending activation","code":"inactive"}`))
		return
	}
	access, _, expiresAt, err := middleware.GenerateTokens(c.jwtSecret, user.ID, user.Username, user.Role, user.SystemRole)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.CookieName,
		Value:    access,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   24 * 60 * 60,
	})
	// Build module permissions map
	perms, _ := c.authHelper.GetUserPermissions(r.Context(), user.ID)
	permMap := map[string]string{}
	for _, p := range perms {
		permMap[p.Module] = p.Permission
	}
	writeJSON(w, map[string]interface{}{
		"expires_at": expiresAt,
		"user": map[string]interface{}{
			"id": user.ID, "username": user.Username,
			"role": user.Role, "system_role": user.SystemRole,
			"display_name": user.DisplayName,
		},
		"module_permissions": permMap,
	})
}

// handleLogout godoc
// @Summary 使用者登出
// @Tags Auth
// @Produce json
// @Success 200 {object} swagOK
// @Router /api/logout [post]
func (c *AuthController) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: middleware.CookieName, Value: "", Path: "/",
		HttpOnly: true, Secure: isSecureRequest(r), MaxAge: -1,
	})
	writeJSON(w, map[string]bool{"ok": true})
}

// handleMe godoc
// @Summary 取得目前登入者資訊
// @Tags Auth
// @Produce json
// @Security BearerAuth
// @Success 200 {object} swagMeResp
// @Failure 401 {object} swagError
// @Router /api/me [get]
func (c *AuthController) handleMe(w http.ResponseWriter, r *http.Request) {
	claims, err := middleware.ExtractTokenFromRequest(r, c.jwtSecret)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	// Build module permissions map
	perms, _ := c.authHelper.GetUserPermissions(r.Context(), claims.UserID)
	permMap := map[string]string{}
	for _, p := range perms {
		permMap[p.Module] = p.Permission
	}
	writeJSON(w, map[string]interface{}{
		"id": claims.UserID, "username": claims.Username,
		"role": claims.Role, "system_role": claims.SystemRole,
		"module_permissions": permMap,
	})
}
