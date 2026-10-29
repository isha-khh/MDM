package domain

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// Error codes of the integration API (spec §1/§7).
const (
	CodeValidationFailed    = "VALIDATION_FAILED"
	CodeUnauthorized        = "UNAUTHORIZED"
	CodeForbidden           = "FORBIDDEN"
	CodeNotFound            = "NOT_FOUND"
	CodeVehicleUnavailable  = "VEHICLE_UNAVAILABLE"
	CodeRegionNotSupported  = "REGION_NOT_SUPPORTED"
	CodeIdempotencyConflict = "IDEMPOTENCY_KEY_CONFLICT"
	CodeDuplicateExternal   = "EXTERNAL_REF_EXISTS"
	CodeBorrowerNotFound    = "BORROWER_NOT_FOUND"
	CodeBorrowerAmbiguous   = "BORROWER_AMBIGUOUS"
	CodeBorrowerInactive    = "BORROWER_INACTIVE"
	CodeNotModifiable       = "RESERVATION_NOT_MODIFIABLE"
	CodeInternal            = "INTERNAL_ERROR"
)

// APIError is a failure the integration API reports to its caller as
// {"error":{"code","message","details"}} with the given HTTP status.
type APIError struct {
	Status  int
	Code    string
	Message string
	Details interface{}
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// Reservation statuses as the dispatch system sees them (spec §7.6).
const (
	ReservationReserved  = "RESERVED"
	ReservationInUse     = "IN_USE"
	ReservationReturned  = "RETURNED"
	ReservationCancelled = "CANCELLED"
)

// ReservationStatus maps a rentals.status to the API's status value.
func ReservationStatus(rentalStatus string) string {
	switch rentalStatus {
	case "active", "pending_return":
		return ReservationInUse
	case "returned":
		return ReservationReturned
	case "cancelled", "rejected":
		return ReservationCancelled
	default: // pending, approved
		return ReservationReserved
	}
}

// ReservationID is the public id of a rental: "R-" and its rental number.
func ReservationID(rentalNumber int) string { return "R-" + strconv.Itoa(rentalNumber) }

// ParseReservationID is the inverse of ReservationID.
func ParseReservationID(id string) (int, bool) {
	rest, ok := strings.CutPrefix(id, "R-")
	if !ok || rest == "" || len(rest) > 9 {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n <= 0 || strconv.Itoa(n) != rest {
		return 0, false
	}
	return n, true
}

// Destination is where the trip goes; its city decides whether the car may be
// reserved at all.
type Destination struct {
	Address  string `json:"address,omitempty"`
	City     string `json:"city,omitempty"`
	District string `json:"district,omitempty"`
}

// Reservation is a rental of an integration resource, as the API shows it.
type Reservation struct {
	RentalNumber int
	ExternalRef  *string
	ResourceID   string
	ServiceType  string
	Status       string // API status, see ReservationStatus
	RentalStatus string // rentals.status
	// BorrowerRef is the borrower identifier the caller supplied (AD account).
	BorrowerRef   *string
	BorrowerSSO   bool // the borrower's account is linked to SSO (its username is then the AD account)
	BorrowerUser  string
	BorrowerEmail string
	BorrowerName  string
	Passengers    []string
	StartAt       time.Time
	EndAt         *time.Time // nil: no end (legacy open-ended rentals)
	Destination   *Destination
	Note          string
	CancelReason  string
	CreatedVia    string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// BorrowerEmployeeID is the AD account the dispatch system knows the borrower
// by: what it sent when the borrower came with the reservation, otherwise the
// username of an SSO-linked account (which is the AD account), otherwise unknown.
func (r *Reservation) BorrowerEmployeeID() *string {
	if r.BorrowerRef != nil && *r.BorrowerRef != "" {
		return r.BorrowerRef
	}
	if r.BorrowerSSO && r.BorrowerUser != "" {
		u := r.BorrowerUser
		return &u
	}
	return nil
}

// IntegrationResource maps an outward resource id (RENTAL_CAR_01) to an asset.
type IntegrationResource struct {
	ResourceID    string
	ServiceType   string
	AssetID       string
	AssetNumber   string
	AssetName     string
	AllowedCities []string
	Enabled       bool
}

// IntegrationClient is a caller of the integration API.
type IntegrationClient struct {
	ID                     string
	Name                   string
	KeyPrefix              string
	Enabled                bool
	IPAllowlist            []string
	AutoCreateEmailDomains []string
	CreatedAt              time.Time
	RotatedAt              *time.Time
	LastUsedAt             *time.Time
}

// NormalizeCity makes city names comparable: Taiwanese place names are written
// with both 台 and 臺, and stray spaces are common in typed addresses.
func NormalizeCity(s string) string {
	return strings.ReplaceAll(strings.Join(strings.Fields(s), ""), "臺", "台")
}

// CityAllowed reports whether city is one of allowed (empty list: anywhere).
func CityAllowed(city string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	c := NormalizeCity(city)
	if c == "" {
		return false
	}
	for _, a := range allowed {
		if NormalizeCity(a) == c {
			return true
		}
	}
	return false
}

// OnMinute reports whether t has no seconds or sub-second part: reservations
// are to-the-minute.
func OnMinute(t time.Time) bool { return t.Second() == 0 && t.Nanosecond() == 0 }

// IPAllowed reports whether ip is in the allowlist (plain addresses or CIDRs).
// An empty allowlist allows every address.
func IPAllowed(ip string, allowlist []string) bool {
	if len(allowlist) == 0 {
		return true
	}
	addr := net.ParseIP(ip)
	if addr == nil {
		return false
	}
	for _, entry := range allowlist {
		entry = strings.TrimSpace(entry)
		if _, n, err := net.ParseCIDR(entry); err == nil {
			if n.Contains(addr) {
				return true
			}
		} else if e := net.ParseIP(entry); e != nil && e.Equal(addr) {
			return true
		}
	}
	return false
}

// ValidateIPEntry checks one allowlist entry (address or CIDR).
func ValidateIPEntry(entry string) error {
	entry = strings.TrimSpace(entry)
	if _, _, err := net.ParseCIDR(entry); err == nil {
		return nil
	}
	if net.ParseIP(entry) != nil {
		return nil
	}
	return fmt.Errorf("%q 不是有效的 IP 或 CIDR", entry)
}

// FormatAPITime renders a moment the way the API speaks: ISO 8601 with the
// Taipei offset (+08:00).
func FormatAPITime(t time.Time) string { return t.In(TaipeiZone).Format(time.RFC3339) }

// ReservationConflict is what a caller learns about a reservation standing in
// its way: enough to see which one and when, nothing about who.
type ReservationConflict struct {
	ReservationID string  `json:"reservationId"`
	ExternalRef   *string `json:"externalRef"`
	Status        string  `json:"status"`
	StartAt       string  `json:"startAt"`
	EndAt         *string `json:"endAt"`
}

func (r *Reservation) Conflict() ReservationConflict {
	c := ReservationConflict{
		ReservationID: ReservationID(r.RentalNumber), ExternalRef: r.ExternalRef,
		Status: r.Status, StartAt: FormatAPITime(r.StartAt),
	}
	if r.EndAt != nil {
		e := FormatAPITime(*r.EndAt)
		c.EndAt = &e
	}
	return c
}
