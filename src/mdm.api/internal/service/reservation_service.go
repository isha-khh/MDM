package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/anthropics/mdm-server/internal/adapter/postgres"
	"github.com/anthropics/mdm-server/internal/domain"
)

// ServiceRentalCar is the one service type the integration API reserves
// (company-owned rental cars; taxis are handled by the administration office).
const ServiceRentalCar = "RENTAL_CAR"

// ReservationUsers is the part of the user store reservations need.
type ReservationUsers interface {
	FindByEmailFold(ctx context.Context, email string) ([]*domain.User, error)
	FindByUsername(ctx context.Context, username string) (*domain.User, error)
}

// ReservationService is the business side of the integration API: it checks
// requests, finds (or creates) the borrower, and books through the store.
type ReservationService struct {
	integration *postgres.IntegrationRepo
	store       *postgres.ReservationRepo
	users       ReservationUsers
}

func NewReservationService(integration *postgres.IntegrationRepo, store *postgres.ReservationRepo, users ReservationUsers) *ReservationService {
	return &ReservationService{integration: integration, store: store, users: users}
}

// CreateReservationRequest is the body of POST /reservations.
type CreateReservationRequest struct {
	ExternalRef string `json:"externalRef"`
	ServiceType string `json:"serviceType"`
	ResourceID  string `json:"resourceId"` // optional: pick one resource of the service type
	// BorrowerEmployeeID is the borrower's AD account. BorrowerEmail is how the
	// borrower is recognised in MDM (the SSO login is matched by email) and is
	// needed to create an account for someone who has not signed in yet.
	BorrowerEmployeeID string              `json:"borrowerEmployeeId"`
	BorrowerEmail      string              `json:"borrowerEmail"`
	BorrowerName       string              `json:"borrowerName"`
	Passengers         []string            `json:"passengers"`
	StartAt            string              `json:"startAt"`
	EndAt              string              `json:"endAt"`
	Destination        *domain.Destination `json:"destination"`
	Note               string              `json:"note"`
}

// UpdateReservationRequest is the body of PUT /reservations/{id}. Fields left
// out keep their value; externalRef, serviceType and resourceId cannot change.
type UpdateReservationRequest struct {
	ExternalRef        *string             `json:"externalRef"`
	ServiceType        *string             `json:"serviceType"`
	ResourceID         *string             `json:"resourceId"`
	BorrowerEmployeeID *string             `json:"borrowerEmployeeId"`
	BorrowerEmail      *string             `json:"borrowerEmail"`
	BorrowerName       *string             `json:"borrowerName"`
	Passengers         *[]string           `json:"passengers"`
	StartAt            *string             `json:"startAt"`
	EndAt              *string             `json:"endAt"`
	Destination        *domain.Destination `json:"destination"`
	Note               *string             `json:"note"`
}

// ReservationView is a reservation as the API returns it (spec §7.5/§7.6).
type ReservationView struct {
	ReservationID      string              `json:"reservationId"`
	ExternalRef        *string             `json:"externalRef"`
	ServiceType        string              `json:"serviceType"`
	ResourceID         string              `json:"resourceId"`
	Status             string              `json:"status"`
	BorrowerEmployeeID *string             `json:"borrowerEmployeeId"`
	BorrowerEmail      string              `json:"borrowerEmail,omitempty"`
	BorrowerName       string              `json:"borrowerName,omitempty"`
	Passengers         []string            `json:"passengers"`
	StartAt            string              `json:"startAt"`
	EndAt              *string             `json:"endAt"`
	Destination        *domain.Destination `json:"destination"`
	Note               string              `json:"note"`
	CancelReason       string              `json:"cancelReason,omitempty"`
	CreatedVia         string              `json:"createdVia"`
	CreatedAt          string              `json:"createdAt"`
	UpdatedAt          string              `json:"updatedAt"`
}

// NewReservationView renders a reservation for the API.
func NewReservationView(r *domain.Reservation) ReservationView {
	v := ReservationView{
		ReservationID: domain.ReservationID(r.RentalNumber), ExternalRef: r.ExternalRef,
		ServiceType: r.ServiceType, ResourceID: r.ResourceID, Status: r.Status,
		BorrowerEmployeeID: r.BorrowerEmployeeID(), BorrowerEmail: r.BorrowerEmail, BorrowerName: r.BorrowerName,
		Passengers: r.Passengers, StartAt: domain.FormatAPITime(r.StartAt),
		Destination: r.Destination, Note: r.Note, CancelReason: r.CancelReason, CreatedVia: r.CreatedVia,
		CreatedAt: domain.FormatAPITime(r.CreatedAt), UpdatedAt: domain.FormatAPITime(r.UpdatedAt),
	}
	if v.Passengers == nil {
		v.Passengers = []string{}
	}
	if r.EndAt != nil {
		e := domain.FormatAPITime(*r.EndAt)
		v.EndAt = &e
	}
	return v
}

// AvailabilityResult is the body of GET /availability.
type AvailabilityResult struct {
	Available  bool                         `json:"available"`
	ResourceID string                       `json:"resourceId"`
	Conflicts  []domain.ReservationConflict `json:"conflicts"`
	Reason     string                       `json:"reason,omitempty"`
}

func invalid(msg string) *domain.APIError {
	return &domain.APIError{Status: http.StatusBadRequest, Code: domain.CodeValidationFailed, Message: msg}
}

// ParseAPITime reads an ISO 8601 time with offset; reservations are to the minute.
func ParseAPITime(field, s string) (time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return time.Time{}, invalid(field + " 必填")
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, invalid(field + " 須為帶時區的 ISO 8601 時間（如 2026-10-30T09:00:00+08:00）")
	}
	if !domain.OnMinute(t) {
		return time.Time{}, invalid(field + " 只到分鐘，秒數須為 0")
	}
	return t.In(domain.TaipeiZone), nil
}

func parseSpan(startName, endName, startS, endS string) (start, end time.Time, err error) {
	if start, err = ParseAPITime(startName, startS); err != nil {
		return
	}
	if end, err = ParseAPITime(endName, endS); err != nil {
		return
	}
	if !end.After(start) {
		err = invalid(endName + " 必須晚於 " + startName)
	}
	return
}

// Availability says whether a service type (or one resource) is free for [from, to).
func (s *ReservationService) Availability(ctx context.Context, serviceType, resourceID, fromS, toS string) (*AvailabilityResult, error) {
	from, to, err := parseSpan("from", "to", fromS, toS)
	if err != nil {
		return nil, err
	}
	if serviceType == "" {
		serviceType = ServiceRentalCar
	}
	candidates, err := s.candidates(ctx, serviceType, resourceID)
	if err != nil {
		return nil, err
	}
	var first *AvailabilityResult
	for _, res := range candidates {
		out := &AvailabilityResult{ResourceID: res.ResourceID, Conflicts: []domain.ReservationConflict{}}
		status, err := s.store.AssetStatus(ctx, res.AssetID)
		if err != nil {
			return nil, err
		}
		if !postgresBookable(status) {
			out.Reason = "ASSET_" + strings.ToUpper(status)
		} else {
			cs, err := s.store.Conflicts(ctx, res.AssetID, from, to)
			if err != nil {
				return nil, err
			}
			for _, c := range cs {
				out.Conflicts = append(out.Conflicts, c.Conflict())
			}
			out.Available = len(cs) == 0
		}
		if out.Available {
			return out, nil
		}
		if first == nil {
			first = out
		}
	}
	return first, nil
}

func postgresBookable(status string) bool { return status == "available" || status == "rented" }

// candidates lists the enabled resources a request may use: the named one, or
// every one of the service type.
func (s *ReservationService) candidates(ctx context.Context, serviceType, resourceID string) ([]*domain.IntegrationResource, error) {
	if resourceID != "" {
		res, err := s.integration.Resource(ctx, resourceID)
		if err != nil {
			return nil, err
		}
		if res == nil || res.ServiceType != serviceType {
			return nil, &domain.APIError{Status: http.StatusNotFound, Code: domain.CodeNotFound, Message: "找不到資源 " + resourceID}
		}
		return []*domain.IntegrationResource{res}, nil
	}
	list, err := s.integration.EnabledResources(ctx, serviceType)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, invalid("serviceType " + serviceType + " 沒有可預約的資源")
	}
	return list, nil
}

func regionError(city string) *domain.APIError {
	msg := "目的地縣市「" + city + "」不在公司租賃車輛的服務範圍"
	if city == "" {
		return invalid("destination.city 必填，才能判定是否在服務範圍")
	}
	return &domain.APIError{Status: http.StatusUnprocessableEntity, Code: domain.CodeRegionNotSupported, Message: msg}
}

func validateCommon(passengers []string, note string) error {
	if len(passengers) > 20 {
		return invalid("passengers 最多 20 人")
	}
	for _, p := range passengers {
		if strings.TrimSpace(p) == "" || len(p) > 100 {
			return invalid("passengers 不可有空白或過長的項目")
		}
	}
	if len(note) > 1000 {
		return invalid("note 過長（上限 1000 字）")
	}
	return nil
}

func cleanPassengers(in []string) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// Create books a reservation. A retry with the same Idempotency-Key replays the first outcome.
func (s *ReservationService) Create(ctx context.Context, actor postgres.Actor, client *domain.IntegrationClient, idem postgres.IdemKey, req CreateReservationRequest) (postgres.WriteResult, bool, error) {
	ref := strings.TrimSpace(req.ExternalRef)
	if ref == "" || len(ref) > 200 {
		return postgres.WriteResult{}, false, invalid("externalRef 必填（長度 200 以內）")
	}
	serviceType := strings.TrimSpace(req.ServiceType)
	if serviceType == "" {
		return postgres.WriteResult{}, false, invalid("serviceType 必填")
	}
	start, end, err := parseSpan("startAt", "endAt", req.StartAt, req.EndAt)
	if err != nil {
		return postgres.WriteResult{}, false, err
	}
	if err := validateCommon(req.Passengers, req.Note); err != nil {
		return postgres.WriteResult{}, false, err
	}

	candidates, err := s.candidates(ctx, serviceType, strings.TrimSpace(req.ResourceID))
	if err != nil {
		return postgres.WriteResult{}, false, err
	}
	city := ""
	if req.Destination != nil {
		city = req.Destination.City
	}
	var allowed []*domain.IntegrationResource
	for _, c := range candidates {
		if domain.CityAllowed(city, c.AllowedCities) {
			allowed = append(allowed, c)
		}
	}
	if len(allowed) == 0 {
		return postgres.WriteResult{}, false, regionError(strings.TrimSpace(city))
	}

	borrower, err := s.resolveBorrower(ctx, client, req.BorrowerEmployeeID, req.BorrowerEmail, req.BorrowerName)
	if err != nil {
		return postgres.WriteResult{}, false, err
	}

	return s.store.Write(ctx, idem, func(tx *postgres.ReservationTx) (postgres.WriteResult, error) {
		var last error
		for _, res := range allowed {
			r, err := tx.Create(ctx, postgres.NewReservation{
				ExternalRef: ref, Resource: res, Borrower: borrower,
				Passengers: cleanPassengers(req.Passengers), Start: start, End: end,
				Destination: req.Destination, Note: req.Note, Actor: actor,
			})
			var ae *domain.APIError
			if errors.As(err, &ae) && ae.Code == domain.CodeVehicleUnavailable && len(allowed) > 1 {
				last = err // try the next resource
				continue
			}
			if err != nil {
				return postgres.WriteResult{}, err
			}
			return renderResult(http.StatusCreated, r)
		}
		return postgres.WriteResult{}, last
	})
}

func renderResult(status int, r *domain.Reservation) (postgres.WriteResult, error) {
	body, err := json.Marshal(NewReservationView(r))
	return postgres.WriteResult{Status: status, Body: body}, err
}

func notFound(id string) error {
	return &domain.APIError{Status: http.StatusNotFound, Code: domain.CodeNotFound, Message: "找不到預約 " + id}
}

// Update changes a reservation that has not been picked up.
func (s *ReservationService) Update(ctx context.Context, actor postgres.Actor, client *domain.IntegrationClient, idem postgres.IdemKey, id string, req UpdateReservationRequest) (postgres.WriteResult, bool, error) {
	number, ok := domain.ParseReservationID(id)
	if !ok {
		return postgres.WriteResult{}, false, notFound(id)
	}
	cur, err := s.store.Get(ctx, number)
	if err != nil {
		return postgres.WriteResult{}, false, err
	}
	if cur == nil {
		return postgres.WriteResult{}, false, notFound(id)
	}
	if req.ExternalRef != nil && (cur.ExternalRef == nil || strings.TrimSpace(*req.ExternalRef) != *cur.ExternalRef) {
		return postgres.WriteResult{}, false, invalid("externalRef 不可變更")
	}
	if req.ServiceType != nil && strings.TrimSpace(*req.ServiceType) != cur.ServiceType {
		return postgres.WriteResult{}, false, invalid("serviceType 不可變更")
	}
	if req.ResourceID != nil && strings.TrimSpace(*req.ResourceID) != cur.ResourceID {
		return postgres.WriteResult{}, false, invalid("resourceId 不可變更")
	}

	patch := postgres.ReservationPatch{Passengers: nil, Destination: req.Destination, Note: req.Note, Actor: actor}
	if req.StartAt != nil || req.EndAt != nil {
		if req.StartAt != nil {
			t, err := ParseAPITime("startAt", *req.StartAt)
			if err != nil {
				return postgres.WriteResult{}, false, err
			}
			patch.Start = &t
		}
		if req.EndAt != nil {
			t, err := ParseAPITime("endAt", *req.EndAt)
			if err != nil {
				return postgres.WriteResult{}, false, err
			}
			patch.End = &t
		}
	}
	if req.Passengers != nil {
		p := cleanPassengers(*req.Passengers)
		patch.Passengers = &p
	}
	note := ""
	if req.Note != nil {
		note = *req.Note
	}
	passengers := []string(nil)
	if req.Passengers != nil {
		passengers = *req.Passengers
	}
	if err := validateCommon(passengers, note); err != nil {
		return postgres.WriteResult{}, false, err
	}
	if req.Destination != nil {
		res, err := s.integration.Resource(ctx, cur.ResourceID)
		if err != nil {
			return postgres.WriteResult{}, false, err
		}
		if res != nil && !domain.CityAllowed(req.Destination.City, res.AllowedCities) {
			return postgres.WriteResult{}, false, regionError(strings.TrimSpace(req.Destination.City))
		}
	}
	if req.BorrowerEmployeeID != nil || req.BorrowerEmail != nil || req.BorrowerName != nil {
		pick := func(p *string) string {
			if p == nil {
				return ""
			}
			return *p
		}
		// Fields left out fall back to what the reservation has.
		empID, email := pick(req.BorrowerEmployeeID), pick(req.BorrowerEmail)
		if req.BorrowerEmployeeID == nil && req.BorrowerEmail == nil {
			return postgres.WriteResult{}, false, invalid("變更借用人需提供 borrowerEmployeeId 或 borrowerEmail")
		}
		b, err := s.resolveBorrower(ctx, client, empID, email, pick(req.BorrowerName))
		if err != nil {
			return postgres.WriteResult{}, false, err
		}
		patch.Borrower = &b
	}
	if patch.Start == nil && patch.End == nil && patch.Borrower == nil && patch.Passengers == nil &&
		patch.Destination == nil && patch.Note == nil {
		return postgres.WriteResult{}, false, invalid("沒有要修改的欄位")
	}

	return s.store.Write(ctx, idem, func(tx *postgres.ReservationTx) (postgres.WriteResult, error) {
		r, err := tx.Update(ctx, number, patch)
		if err != nil {
			return postgres.WriteResult{}, err
		}
		return renderResult(http.StatusOK, r)
	})
}

// Cancel cancels a reservation that has not been picked up.
func (s *ReservationService) Cancel(ctx context.Context, actor postgres.Actor, idem postgres.IdemKey, id, reason string) (postgres.WriteResult, bool, error) {
	number, ok := domain.ParseReservationID(id)
	if !ok {
		return postgres.WriteResult{}, false, notFound(id)
	}
	if len(reason) > 500 {
		return postgres.WriteResult{}, false, invalid("reason 過長（上限 500 字）")
	}
	return s.store.Write(ctx, idem, func(tx *postgres.ReservationTx) (postgres.WriteResult, error) {
		r, err := tx.Cancel(ctx, number, strings.TrimSpace(reason), actor)
		if err != nil {
			return postgres.WriteResult{}, err
		}
		return renderResult(http.StatusOK, r)
	})
}

// Get returns one reservation.
func (s *ReservationService) Get(ctx context.Context, id string) (*ReservationView, error) {
	number, ok := domain.ParseReservationID(id)
	if !ok {
		return nil, notFound(id)
	}
	r, err := s.store.Get(ctx, number)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, notFound(id)
	}
	v := NewReservationView(r)
	return &v, nil
}

// ListByExternalRef returns the reservations made for a caller's reference.
func (s *ReservationService) ListByExternalRef(ctx context.Context, ref string) ([]ReservationView, error) {
	rs, err := s.store.FindByExternalRef(ctx, strings.TrimSpace(ref))
	return views(rs), err
}

// ListWindow returns the reservations overlapping [from, to), for reconciliation.
func (s *ReservationService) ListWindow(ctx context.Context, fromS, toS, resourceID string) ([]ReservationView, error) {
	from, to, err := parseSpan("from", "to", fromS, toS)
	if err != nil {
		return nil, err
	}
	rs, err := s.store.ListWindow(ctx, from, to, strings.TrimSpace(resourceID))
	return views(rs), err
}

func views(rs []*domain.Reservation) []ReservationView {
	out := make([]ReservationView, 0, len(rs))
	for _, r := range rs {
		out = append(out, NewReservationView(r))
	}
	return out
}

// resolveBorrower finds the MDM user a reservation is for.
//
// The email is the reliable key: SSO logins are matched by it. The AD account
// (employee id) is only trusted against a username when the account is linked
// to SSO — local accounts have usernames that mean nothing outside MDM. When
// nobody matches, an account is created if the email is in a domain the client
// may create accounts for; otherwise the request is refused rather than guessed.
func (s *ReservationService) resolveBorrower(ctx context.Context, client *domain.IntegrationClient, employeeID, email, name string) (postgres.Borrower, error) {
	employeeID = strings.TrimSpace(employeeID)
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" && strings.Contains(employeeID, "@") {
		email, employeeID = strings.ToLower(employeeID), ""
	}
	if employeeID == "" && email == "" {
		return postgres.Borrower{}, invalid("borrowerEmployeeId 或 borrowerEmail 必填")
	}
	if email != "" {
		if a, err := mail.ParseAddress(email); err != nil || a.Address != email {
			return postgres.Borrower{}, invalid("borrowerEmail 格式不正確")
		}
	}
	borrowerErr := func(status int, code, msg string) error {
		return &domain.APIError{Status: status, Code: code, Message: msg}
	}
	use := func(u *domain.User) (postgres.Borrower, error) {
		if !u.IsActive {
			return postgres.Borrower{}, borrowerErr(http.StatusUnprocessableEntity, domain.CodeBorrowerInactive, "借用人帳號已停用")
		}
		display := u.DisplayName
		if display == "" {
			display = u.Username
		}
		return postgres.Borrower{UserID: u.ID, Name: display, Ref: employeeID}, nil
	}

	if email != "" {
		users, err := s.users.FindByEmailFold(ctx, email)
		if err != nil {
			return postgres.Borrower{}, err
		}
		switch len(users) {
		case 0:
		case 1:
			return use(users[0])
		default:
			return postgres.Borrower{}, borrowerErr(http.StatusUnprocessableEntity, domain.CodeBorrowerAmbiguous,
				"有多個帳號使用同一個 email，請管理者先整理")
		}
	}
	if employeeID != "" {
		u, err := s.users.FindByUsername(ctx, employeeID)
		if err != nil {
			return postgres.Borrower{}, err
		}
		if u != nil && u.SSOSub != "" && (email == "" || u.Email == "" || strings.EqualFold(u.Email, email)) {
			return use(u)
		}
	}

	who := employeeID
	if who == "" {
		who = email
	}
	if email == "" {
		return postgres.Borrower{}, borrowerErr(http.StatusUnprocessableEntity, domain.CodeBorrowerNotFound,
			"找不到借用人 "+who+"；請同時提供 borrowerEmail 以便建立帳號")
	}
	at := strings.LastIndex(email, "@")
	domainPart := email[at+1:]
	canCreate := false
	for _, d := range client.AutoCreateEmailDomains {
		if strings.EqualFold(strings.TrimSpace(d), domainPart) {
			canCreate = true
		}
	}
	if !canCreate {
		return postgres.Borrower{}, borrowerErr(http.StatusUnprocessableEntity, domain.CodeBorrowerNotFound,
			"找不到借用人 "+who+"，且其 email 網域不在自動建立帳號的範圍內")
	}
	username := employeeID
	if username == "" {
		username = email[:at]
	}
	display := strings.TrimSpace(name)
	if display == "" {
		display = email[:at]
	}
	return postgres.Borrower{
		Create: &postgres.NewBorrower{Username: username, DisplayName: display, Email: email},
		Name:   display, Ref: employeeID,
	}, nil
}
