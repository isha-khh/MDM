package domain

import "time"

// TaipeiZone is fixed UTC+8: Taiwan has no daylight saving, so this is exact
// and needs no time zone database.
var TaipeiZone = time.FixedZone("Asia/Taipei", 8*3600)

// RentalWindow is the span of time a booking made with a borrow date, an
// expected return date and a period occupies. It mirrors the rental_span()
// SQL function (migration 042) for date-based bookings, so what the server
// checks for a new booking is what it later compares against stored rentals.
//
// start is inclusive, end exclusive; a nil end means open-ended (no expected
// return date: the asset is held indefinitely). Dates are read the way the
// rest of the rental code reads them, as the UTC calendar date of the stored
// value, and turned into Taipei-time boundaries:
//
//	full  [borrow 00:00, return+1 day 00:00)
//	am    [borrow 00:00, return 12:00)
//	pm    [borrow 12:00, return+1 day 00:00)
func RentalWindow(period string, borrowDate time.Time, expectedReturn *time.Time) (start time.Time, end *time.Time) {
	by, bm, bd := borrowDate.UTC().Date()
	start = time.Date(by, bm, bd, 0, 0, 0, 0, TaipeiZone)
	if period == "pm" {
		start = start.Add(12 * time.Hour)
	}
	if expectedReturn == nil {
		return start, nil
	}
	ry, rm, rd := expectedReturn.UTC().Date()
	var e time.Time
	if period == "am" {
		e = time.Date(ry, rm, rd, 12, 0, 0, 0, TaipeiZone)
	} else {
		e = time.Date(ry, rm, rd+1, 0, 0, 0, 0, TaipeiZone)
	}
	if e.Before(start) {
		e = start // return before borrow: empty, as in SQL
	}
	return start, &e
}

// Booking describes a rental about to be made, for checking it against the
// asset's open rentals.
type Booking struct {
	Period     string
	BorrowDate time.Time
	// Start/End is the span the booking occupies (End nil: open-ended).
	Start time.Time
	End   *time.Time
	// Reservation: the asset's category allows advance reservations, so only
	// time-span overlap counts. Otherwise any open rental of the asset blocks
	// the booking, as it always did.
	Reservation bool
}

// NewBooking builds a Booking from a date-based request.
func NewBooking(period string, borrowDate time.Time, expectedReturn *time.Time, reservation bool) Booking {
	start, end := RentalWindow(period, borrowDate, expectedReturn)
	return Booking{Period: period, BorrowDate: borrowDate, Start: start, End: end, Reservation: reservation}
}

// PickableStatus is the status the rental picker shows for an asset.
//
// Categories without reservations keep the old rule: any open whole-day rental
// makes the asset "rented". Categories with reservations show "rented" only
// while something actually holds it right now — a booking for a later date
// doesn't make it unavailable today (and the picker still lets it be reserved
// while it is out). Other statuses (faulty, retired ...) always win.
func PickableStatus(assetStatus string, reservation, heldNow, openWholeDay bool) string {
	if assetStatus != "" && assetStatus != "available" && assetStatus != "rented" {
		return assetStatus
	}
	rented := openWholeDay
	if reservation {
		rented = heldNow
	}
	if rented {
		return "rented"
	}
	if assetStatus == "" || assetStatus == "rented" {
		return "available"
	}
	return assetStatus
}
