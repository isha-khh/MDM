package postgres

import (
	"testing"
	"time"

	"github.com/anthropics/mdm-server/internal/domain"
)

func TestReservationDates(t *testing.T) {
	tp := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, domain.TaipeiZone) }
	utcDay := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }

	// The Taipei calendar date, stored as UTC midnight — even just after midnight,
	// when UTC is still the day before.
	if got := dateUTC(tp(30, 0, 30)); !got.Equal(utcDay(30)) {
		t.Errorf("00:30 Taipei -> %v", got)
	}
	if got := dateUTC(tp(30, 23, 59)); !got.Equal(utcDay(30)) {
		t.Errorf("23:59 Taipei -> %v", got)
	}
	// A span's last day: the day of its end, unless it ends exactly at midnight.
	for name, c := range map[string]struct {
		start, end time.Time
		want       time.Time
	}{
		"same day":             {tp(30, 9, 0), tp(30, 17, 0), utcDay(30)},
		"next morning":         {tp(30, 9, 0), tp(31, 8, 0), utcDay(31)},
		"ends at midnight":     {tp(30, 9, 0), tp(31, 0, 0), utcDay(30)},
		"one minute past":      {tp(30, 9, 0), tp(31, 0, 1), utcDay(31)},
		"two days to midnight": {tp(30, 9, 0), tp(32, 0, 0), utcDay(31)},
	} {
		if got := returnDate(c.start, c.end); !got.Equal(c.want) {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}
