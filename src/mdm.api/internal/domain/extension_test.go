package domain

import (
	"errors"
	"testing"
	"time"
)

func TestValidateExtensionRequest(t *testing.T) {
	d := func(m time.Month, day int) time.Time { return time.Date(2026, m, day, 0, 0, 0, 0, time.UTC) }
	due := d(10, 10)
	today := d(10, 8)

	cases := []struct {
		name      string
		status    string
		due       *time.Time
		requested time.Time
		today     time.Time
		reason    string
		want      error
	}{
		{"valid extension", "active", &due, d(10, 15), today, "出差延後", nil},
		{"one day later is enough", "active", &due, d(10, 11), today, "x", nil},
		{"pending rental can't renew", "pending", &due, d(10, 15), today, "x", ErrExtensionNotActive},
		{"approved (not handed out) can't renew", "approved", &due, d(10, 15), today, "x", ErrExtensionNotActive},
		{"already reported return can't renew", "pending_return", &due, d(10, 15), today, "x", ErrExtensionNotActive},
		{"returned can't renew", "returned", &due, d(10, 15), today, "x", ErrExtensionNotActive},
		{"no due date", "active", nil, d(10, 15), today, "x", ErrExtensionNoDueDate},
		{"empty reason", "active", &due, d(10, 15), today, "", ErrExtensionReasonEmpty},
		{"whitespace-only reason", "active", &due, d(10, 15), today, "  \t ", ErrExtensionReasonEmpty},
		{"same date as current", "active", &due, d(10, 10), today, "x", ErrExtensionNotLater},
		{"earlier than current", "active", &due, d(10, 9), today, "x", ErrExtensionNotLater},
		// Already overdue (due Oct 10, today Oct 14): Oct 12 is later than the
		// due date but still in the past.
		{"overdue, new date still in the past", "active", &due, d(10, 12), d(10, 14), "x", ErrExtensionInPast},
		{"overdue, new date today", "active", &due, d(10, 14), d(10, 14), "x", nil},
		{"overdue, new date in the future", "active", &due, d(10, 20), d(10, 14), "x", nil},
	}
	for _, c := range cases {
		got := ValidateExtensionRequest(c.status, c.due, c.requested, c.today, c.reason)
		if !errors.Is(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestValidateExtensionRequest_IgnoresTimeOfDayAndZone(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Taipei")
	// expected_return arrives from a DATE column as UTC midnight; "today" is
	// a local wall-clock time late in the evening. Only the calendar days count.
	due := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	requested := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	today := time.Date(2026, 10, 10, 23, 59, 0, 0, loc)
	if err := ValidateExtensionRequest("active", &due, requested, today, "x"); err != nil {
		t.Errorf("should be valid, got %v", err)
	}
}
