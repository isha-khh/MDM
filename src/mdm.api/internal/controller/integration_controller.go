package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/anthropics/mdm-server/internal/adapter/postgres"
	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/service"
)

// IntegrationController is the reservation API the dispatch system calls
// (spec §7). It is served on its own listener — separate from the web UI's
// API — and authenticated by API key only.
type IntegrationController struct {
	svc  *service.ReservationService
	auth *middleware.IntegrationAuth
}

func NewIntegrationController(svc *service.ReservationService, auth *middleware.IntegrationAuth) *IntegrationController {
	return &IntegrationController{svc: svc, auth: auth}
}

const maxIntegrationBody = 1 << 20

// Handler returns the routes of the integration listener.
func (c *IntegrationController) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /api/v1/availability", c.authed(c.availability))
	mux.HandleFunc("POST /api/v1/reservations", c.authed(c.create))
	mux.HandleFunc("GET /api/v1/reservations", c.authed(c.list))
	mux.HandleFunc("GET /api/v1/reservations/{id}", c.authed(c.get))
	mux.HandleFunc("PUT /api/v1/reservations/{id}", c.authed(c.update))
	mux.HandleFunc("POST /api/v1/reservations/{id}/cancel", c.authed(c.cancel))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeAPIError(w, &domain.APIError{Status: http.StatusNotFound, Code: domain.CodeNotFound, Message: "no such endpoint"})
	})
	return mux
}

type authedHandler func(w http.ResponseWriter, r *http.Request, client *domain.IntegrationClient, actor postgres.Actor)

func (c *IntegrationController) authed(h authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, ip, err := c.auth.Authenticate(r)
		if err != nil {
			writeAPIError(w, err)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxIntegrationBody)
		h(w, r, client, postgres.Actor{ClientID: client.ID, ClientName: client.Name, IP: ip})
	}
}

func writeAPIError(w http.ResponseWriter, err error) {
	var ae *domain.APIError
	if !errors.As(err, &ae) {
		log.Printf("[integration-api] internal error: %v", err)
		ae = &domain.APIError{Status: http.StatusInternalServerError, Code: domain.CodeInternal, Message: "內部錯誤"}
	}
	details := ae.Details
	if details == nil {
		details = []interface{}{}
	}
	writeAPIJSON(w, ae.Status, map[string]interface{}{"error": map[string]interface{}{
		"code": ae.Code, "message": ae.Message, "details": details,
	}})
}

func writeAPIJSON(w http.ResponseWriter, status int, v interface{}) {
	body, _ := json.Marshal(v)
	writeAPIBody(w, status, body, false)
}

func writeAPIBody(w http.ResponseWriter, status int, body []byte, replayed bool) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	w.WriteHeader(status)
	w.Write(body)
}

func badRequest(msg string) error {
	return &domain.APIError{Status: http.StatusBadRequest, Code: domain.CodeValidationFailed, Message: msg}
}

// readJSON reads the request body into v, and returns a canonical form of it
// (keys sorted, whitespace gone) for the idempotency fingerprint. An empty
// body is allowed when optional.
func readJSON(r *http.Request, v interface{}, optional bool) (canonical []byte, err error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, badRequest("請求內容過大或無法讀取")
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		if optional {
			return []byte("null"), nil
		}
		return nil, badRequest("請求內容不可為空")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return nil, badRequest("請求內容不是有效的 JSON 物件：" + err.Error())
	}
	var generic interface{}
	json.Unmarshal(raw, &generic)
	canonical, _ = json.Marshal(generic)
	return canonical, nil
}

// idemKey checks the Idempotency-Key header that every write must carry and
// fingerprints the request so a reused key with a different request is caught.
func idemKey(r *http.Request, client *domain.IntegrationClient, canonicalBody []byte) (postgres.IdemKey, error) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		return postgres.IdemKey{}, badRequest("寫入類 API 必須帶 Idempotency-Key header")
	}
	if len(key) > 255 {
		return postgres.IdemKey{}, badRequest("Idempotency-Key 過長（上限 255）")
	}
	sum := sha256.Sum256([]byte(r.Method + " " + r.URL.Path + "\n" + string(canonicalBody)))
	return postgres.IdemKey{ClientID: client.ID, Key: key, Hash: hex.EncodeToString(sum[:])}, nil
}

func (c *IntegrationController) availability(w http.ResponseWriter, r *http.Request, _ *domain.IntegrationClient, _ postgres.Actor) {
	q := r.URL.Query()
	res, err := c.svc.Availability(r.Context(), q.Get("serviceType"), q.Get("resourceId"), q.Get("from"), q.Get("to"))
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, res)
}

func (c *IntegrationController) create(w http.ResponseWriter, r *http.Request, client *domain.IntegrationClient, actor postgres.Actor) {
	var req service.CreateReservationRequest
	canon, err := readJSON(r, &req, false)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	idem, err := idemKey(r, client, canon)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	res, replayed, err := c.svc.Create(r.Context(), actor, client, idem, req)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeAPIBody(w, res.Status, res.Body, replayed)
}

func (c *IntegrationController) update(w http.ResponseWriter, r *http.Request, client *domain.IntegrationClient, actor postgres.Actor) {
	var req service.UpdateReservationRequest
	canon, err := readJSON(r, &req, false)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	idem, err := idemKey(r, client, canon)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	res, replayed, err := c.svc.Update(r.Context(), actor, client, idem, r.PathValue("id"), req)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeAPIBody(w, res.Status, res.Body, replayed)
}

func (c *IntegrationController) cancel(w http.ResponseWriter, r *http.Request, client *domain.IntegrationClient, actor postgres.Actor) {
	var body struct {
		Reason string `json:"reason"`
	}
	canon, err := readJSON(r, &body, true)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	idem, err := idemKey(r, client, canon)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	res, replayed, err := c.svc.Cancel(r.Context(), actor, idem, r.PathValue("id"), body.Reason)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeAPIBody(w, res.Status, res.Body, replayed)
}

func (c *IntegrationController) get(w http.ResponseWriter, r *http.Request, _ *domain.IntegrationClient, _ postgres.Actor) {
	v, err := c.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, v)
}

// list answers both queries of spec §7.5: by externalRef (to check whether a
// timed-out create went through) and by time window (daily reconciliation).
func (c *IntegrationController) list(w http.ResponseWriter, r *http.Request, _ *domain.IntegrationClient, _ postgres.Actor) {
	q := r.URL.Query()
	var (
		out []service.ReservationView
		err error
	)
	switch {
	case q.Get("externalRef") != "":
		out, err = c.svc.ListByExternalRef(r.Context(), q.Get("externalRef"))
	case q.Get("from") != "" || q.Get("to") != "":
		out, err = c.svc.ListWindow(r.Context(), q.Get("from"), q.Get("to"), q.Get("resourceId"))
	default:
		err = badRequest("需要 externalRef，或 from 與 to 查詢參數")
	}
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]interface{}{"reservations": out})
}
