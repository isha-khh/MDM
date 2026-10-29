package domain

import (
	"testing"
	"time"
)

func d(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.UTC) }

func at(y int, m time.Month, day, h int) time.Time {
	return time.Date(y, m, day, h, 0, 0, 0, TaipeiZone)
}

func TestRentalWindow(t *testing.T) {
	oct30, nov1, oct28 := d(2026, 10, 30), d(2026, 11, 1), d(2026, 10, 28)
	cases := []struct {
		name   string
		period string
		borrow time.Time
		ret    *time.Time
		start  time.Time
		end    *time.Time // nil = open-ended
	}{
		{"full one day", "full", oct30, &oct30, at(2026, 10, 30, 0), ptr(at(2026, 10, 31, 0))},
		{"full three days", "full", oct30, &nov1, at(2026, 10, 30, 0), ptr(at(2026, 11, 2, 0))},
		{"morning", "am", oct30, &oct30, at(2026, 10, 30, 0), ptr(at(2026, 10, 30, 12))},
		{"afternoon", "pm", oct30, &oct30, at(2026, 10, 30, 12), ptr(at(2026, 10, 31, 0))},
		{"open ended", "full", oct30, nil, at(2026, 10, 30, 0), nil},
		{"return before borrow is empty", "full", oct30, &oct28, at(2026, 10, 30, 0), ptr(at(2026, 10, 30, 0))},
		{"month end rolls over", "full", d(2026, 10, 31), ptr(d(2026, 10, 31)), at(2026, 10, 31, 0), ptr(at(2026, 11, 1, 0))},
	}
	for _, c := range cases {
		s, e := RentalWindow(c.period, c.borrow, c.ret)
		if !s.Equal(c.start) {
			t.Errorf("%s: start %v, want %v", c.name, s, c.start)
		}
		switch {
		case c.end == nil && e != nil:
			t.Errorf("%s: end %v, want open-ended", c.name, *e)
		case c.end != nil && (e == nil || !e.Equal(*c.end)):
			t.Errorf("%s: end %v, want %v", c.name, e, *c.end)
		}
	}
}

func TestRentalWindow_ReadsTheUTCDateLikeTheDatabase(t *testing.T) {
	// A borrow date stored as UTC midnight must land on the same calendar day in
	// Taipei, not the day before (UTC midnight is 08:00 in Taipei the same day).
	s, _ := RentalWindow("full", d(2026, 10, 30), nil)
	if !s.Equal(at(2026, 10, 30, 0)) {
		t.Errorf("start %v", s)
	}
	// A time late in the UTC day is still that UTC date, matching
	// (borrow_date AT TIME ZONE 'UTC')::date in SQL.
	late := time.Date(2026, 10, 30, 23, 30, 0, 0, time.UTC)
	if s, _ := RentalWindow("full", late, nil); !s.Equal(at(2026, 10, 30, 0)) {
		t.Errorf("late-UTC start %v", s)
	}
}

func ptr(t time.Time) *time.Time { return &t }

func TestPickableStatus(t *testing.T) {
	cases := []struct {
		name                        string
		stored                      string
		reservation, heldNow, whole bool
		want                        string
	}{
		{"free", "available", false, false, false, "available"},
		{"free, empty stored status", "", false, false, false, "available"},
		{"no reservations: any open whole-day rental", "available", false, false, true, "rented"},
		{"no reservations: held now", "available", false, true, true, "rented"},
		{"reservations: booked later only", "available", true, false, true, "available"},
		{"reservations: held right now", "available", true, true, true, "rented"},
		{"faulty always wins", "faulty", true, true, true, "faulty"},
		{"retired always wins", "retired", false, false, false, "retired"},
	}
	for _, c := range cases {
		if got := PickableStatus(c.stored, c.reservation, c.heldNow, c.whole); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestNewBooking(t *testing.T) {
	bd := d(2026, 10, 30)
	b := NewBooking("full", bd, &bd, true)
	if !b.Reservation || !b.Start.Equal(at(2026, 10, 30, 0)) || b.End == nil || !b.End.Equal(at(2026, 10, 31, 0)) || b.Period != "full" {
		t.Errorf("%+v", b)
	}
}
