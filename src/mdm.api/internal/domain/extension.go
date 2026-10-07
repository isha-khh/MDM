package domain

import (
	"errors"
	"strings"
	"time"
)

// RentalExtension is a borrower's request to push a rental batch's
// expected_return later. It only takes effect once a custodian/admin
// approves it; until then the rental keeps its original date.
type RentalExtension struct {
	ID                      string
	RentalNumber            int
	RequestedBy             string
	RequestedByName         string
	PreviousExpectedReturn  time.Time
	RequestedExpectedReturn time.Time
	Reason                  string
	Status                  string // pending, approved, rejected
	DecidedBy               *string
	DecidedAt               *time.Time
	DecisionNote            string
	CreatedAt               time.Time
}

var (
	ErrExtensionNotActive   = errors.New("只有借出中的租借可以續借")
	ErrExtensionNoDueDate   = errors.New("此租借沒有預計歸還日期，不需要續借")
	ErrExtensionReasonEmpty = errors.New("請填寫續借原因")
	ErrExtensionNotLater    = errors.New("新的歸還日期必須晚於目前的預計歸還日期")
	ErrExtensionInPast      = errors.New("新的歸還日期不能早於今天")
)

func ymd(t time.Time) int {
	y, m, d := t.Date()
	return y*10000 + int(m)*100 + d
}

// ValidateExtensionRequest checks a renewal request against the rental's
// current state. Dates are compared as calendar days only (expected_return is
// a DATE column), so time-of-day and zone offsets never matter. `today` is
// passed in rather than read from the clock so the rule is testable.
func ValidateExtensionRequest(status string, currentDue *time.Time, requested, today time.Time, reason string) error {
	if status != "active" {
		return ErrExtensionNotActive
	}
	if currentDue == nil {
		return ErrExtensionNoDueDate
	}
	if strings.TrimSpace(reason) == "" {
		return ErrExtensionReasonEmpty
	}
	if ymd(requested) <= ymd(*currentDue) {
		return ErrExtensionNotLater
	}
	// An already-overdue rental can't be "renewed" to a date that has also
	// already passed — that would just paper over the overrun.
	if ymd(requested) < ymd(today) {
		return ErrExtensionInPast
	}
	return nil
}
