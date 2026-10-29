package domain

import (
	"testing"
	"time"
)

func TestReservationID(t *testing.T) {
	if got := ReservationID(1001); got != "R-1001" {
		t.Fatal(got)
	}
	for id, want := range map[string]int{"R-1": 1, "R-1001": 1001} {
		if n, ok := ParseReservationID(id); !ok || n != want {
			t.Errorf("%s -> %d %v", id, n, ok)
		}
	}
	for _, bad := range []string{"", "R-", "R-0", "R--1", "R-01", "R-1a", "r-1", "1001", "R-1 ", "R-12345678901"} {
		if _, ok := ParseReservationID(bad); ok {
			t.Errorf("%q should not parse", bad)
		}
	}
}

func TestReservationStatus(t *testing.T) {
	for rental, want := range map[string]string{
		"pending": ReservationReserved, "approved": ReservationReserved,
		"active": ReservationInUse, "pending_return": ReservationInUse,
		"returned": ReservationReturned, "cancelled": ReservationCancelled, "rejected": ReservationCancelled,
	} {
		if got := ReservationStatus(rental); got != want {
			t.Errorf("%s -> %s, want %s", rental, got, want)
		}
	}
}

func TestCityAllowed(t *testing.T) {
	allowed := []string{"台南市", "高雄市", "屏東縣"}
	for city, want := range map[string]bool{
		"高雄市": true, "臺南市": true, " 台 南 市 ": true, "屏東縣": true,
		"台北市": false, "新北市": false, "": false, "高雄": false,
	} {
		if got := CityAllowed(city, allowed); got != want {
			t.Errorf("%q = %v, want %v", city, got, want)
		}
	}
	if !CityAllowed("台北市", nil) || !CityAllowed("", []string{}) {
		t.Error("an empty list allows anywhere")
	}
	if !CityAllowed("台南市", []string{"臺南市"}) {
		t.Error("the configured list is normalised too")
	}
}

func TestOnMinute(t *testing.T) {
	base := time.Date(2026, 10, 30, 9, 0, 0, 0, TaipeiZone)
	if !OnMinute(base) || OnMinute(base.Add(time.Second)) || OnMinute(base.Add(time.Millisecond)) {
		t.Fatal("seconds and fractions are not to the minute")
	}
}

func TestIPAllowed(t *testing.T) {
	list := []string{"203.0.113.0/24", "198.51.100.7", "2001:db8::/32", " 192.0.2.1 "}
	for ip, want := range map[string]bool{
		"203.0.113.5": true, "203.0.114.5": false, "198.51.100.7": true, "198.51.100.8": false,
		"2001:db8::1": true, "2001:db9::1": false, "192.0.2.1": true, "not-an-ip": false, "": false,
	} {
		if got := IPAllowed(ip, list); got != want {
			t.Errorf("%q = %v, want %v", ip, got, want)
		}
	}
	if !IPAllowed("anything", nil) {
		t.Error("an empty allowlist allows every address")
	}
}

func TestValidateIPEntry(t *testing.T) {
	for _, ok := range []string{"1.2.3.4", "10.0.0.0/8", "::1", "2001:db8::/32"} {
		if err := ValidateIPEntry(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "1.2.3", "1.2.3.4/33", "example.com", "1.2.3.4-5"} {
		if ValidateIPEntry(bad) == nil {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestBorrowerEmployeeID(t *testing.T) {
	ref := "wang.xm"
	cases := []struct {
		name string
		r    Reservation
		want string // "" = null
	}{
		{"what the caller sent wins", Reservation{BorrowerRef: &ref, BorrowerSSO: true, BorrowerUser: "other"}, "wang.xm"},
		{"SSO account: the username is the AD account", Reservation{BorrowerSSO: true, BorrowerUser: "lee.xh"}, "lee.xh"},
		{"local account: unknown", Reservation{BorrowerUser: "local"}, ""},
	}
	for _, c := range cases {
		got := c.r.BorrowerEmployeeID()
		if (got == nil) != (c.want == "") || (got != nil && *got != c.want) {
			t.Errorf("%s: got %v, want %q", c.name, got, c.want)
		}
	}
}

func TestConflictRendersTaipeiTime(t *testing.T) {
	end := time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC) // 09:00 in Taipei
	r := &Reservation{RentalNumber: 7, StartAt: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), EndAt: &end, Status: ReservationReserved}
	c := r.Conflict()
	if c.ReservationID != "R-7" || c.StartAt != "2026-10-30T08:00:00+08:00" || *c.EndAt != "2026-10-30T09:00:00+08:00" {
		t.Fatalf("%+v", c)
	}
}
