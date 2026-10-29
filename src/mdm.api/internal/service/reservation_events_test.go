package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/mdm-server/internal/domain"
)

func res(mut func(r *domain.Reservation)) *domain.Reservation {
	start := time.Date(2026, 10, 30, 9, 0, 0, 0, domain.TaipeiZone)
	end := time.Date(2026, 10, 30, 17, 0, 0, 0, domain.TaipeiZone)
	ref := "protrack:P-88"
	user := "wang.xm"
	r := &domain.Reservation{
		RentalNumber: 1001, ExternalRef: &ref, ResourceID: "RENTAL_CAR_01", ServiceType: "RENTAL_CAR",
		Status: domain.ReservationReserved, RentalStatus: "approved", BorrowerUser: user, BorrowerSSO: true,
		BorrowerEmail: "wang.xm@mail.isha.org.tw", BorrowerName: "王小明",
		Passengers: []string{"wang.xm", "lee.xh"}, StartAt: start, EndAt: &end,
	}
	if mut != nil {
		mut(r)
	}
	return r
}

func TestClassifyChange(t *testing.T) {
	later := time.Date(2026, 10, 30, 18, 0, 0, 0, domain.TaipeiZone)
	cases := []struct {
		name          string
		before, after *domain.Reservation
		want          string // "" = no event
		wantStatus    string // status reported
	}{
		{"nothing at all", nil, nil, "", ""},
		{"new reservation", nil, res(nil), EventCreated, "RESERVED"},
		{"nothing changed", res(nil), res(nil), "", ""},
		{"approved: still reserved", res(func(r *domain.Reservation) { r.RentalStatus = "pending" }), res(nil), "", ""},
		{"later end", res(nil), res(func(r *domain.Reservation) { r.EndAt = &later }), EventChanged, "RESERVED"},
		{"earlier start", res(nil), res(func(r *domain.Reservation) { r.StartAt = r.StartAt.Add(-time.Hour) }), EventChanged, "RESERVED"},
		{"other borrower", res(nil), res(func(r *domain.Reservation) { r.BorrowerEmail = "lee.xh@mail.isha.org.tw" }), EventChanged, "RESERVED"},
		{"other riders", res(nil), res(func(r *domain.Reservation) { r.Passengers = []string{"wang.xm"} }), EventChanged, "RESERVED"},
		{"other car", res(nil), res(func(r *domain.Reservation) { r.ResourceID = "RENTAL_CAR_02" }), EventChanged, "RESERVED"},
		{"handed over", res(nil), res(func(r *domain.Reservation) { r.Status = domain.ReservationInUse }), EventStatusChanged, "IN_USE"},
		{"handed back", res(func(r *domain.Reservation) { r.Status = domain.ReservationInUse }),
			res(func(r *domain.Reservation) { r.Status = domain.ReservationReturned }), EventStatusChanged, "RETURNED"},
		{"extended while out", res(func(r *domain.Reservation) { r.Status = domain.ReservationInUse }),
			res(func(r *domain.Reservation) { r.Status = domain.ReservationInUse; r.EndAt = &later }), EventChanged, "IN_USE"},
		{"cancelled", res(nil), res(func(r *domain.Reservation) { r.Status = domain.ReservationCancelled }), EventCancelled, "CANCELLED"},
		{"cancelled twice", res(func(r *domain.Reservation) { r.Status = domain.ReservationCancelled }),
			res(func(r *domain.Reservation) { r.Status = domain.ReservationCancelled }), "", ""},
		{"deleted", res(nil), nil, EventCancelled, "CANCELLED"},
		{"deleted after cancel", res(func(r *domain.Reservation) { r.Status = domain.ReservationCancelled }), nil, "", ""},
	}
	for _, c := range cases {
		got, report, ok := ClassifyChange(c.before, c.after)
		if (c.want == "") == ok || got != c.want {
			t.Errorf("%s: got %q ok=%v, want %q", c.name, got, ok, c.want)
			continue
		}
		if ok && report.Status != c.wantStatus {
			t.Errorf("%s: reports status %s, want %s", c.name, report.Status, c.wantStatus)
		}
	}
}

func TestBuildEvent_MatchesTheSpecShape(t *testing.T) {
	before := res(nil)
	after := res(func(r *domain.Reservation) {
		e := time.Date(2026, 10, 30, 18, 0, 0, 0, domain.TaipeiZone)
		r.StartAt = time.Date(2026, 10, 30, 13, 0, 0, 0, domain.TaipeiZone)
		r.EndAt = &e
	})
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, domain.TaipeiZone)
	ev := BuildEvent("mdm-evt-20261029-000123", EventChanged, before, after, "admin.chen", "車輛送修，延後至下午出發", now)
	raw, _ := json.Marshal(ev)
	var m map[string]interface{}
	json.Unmarshal(raw, &m)

	for k, want := range map[string]interface{}{
		"eventId": "mdm-evt-20261029-000123", "eventType": "reservation.changed", "occurredAt": "2026-10-29T10:00:00+08:00",
		"reservationId": "R-1001", "externalRef": "protrack:P-88", "changedBy": "admin.chen", "note": "車輛送修，延後至下午出發",
	} {
		if m[k] != want {
			t.Errorf("%s = %v, want %v", k, m[k], want)
		}
	}
	r := m["reservation"].(map[string]interface{})
	p := m["previous"].(map[string]interface{})
	if r["status"] != "RESERVED" || r["resourceId"] != "RENTAL_CAR_01" || r["borrowerEmployeeId"] != "wang.xm" ||
		r["startAt"] != "2026-10-30T13:00:00+08:00" || r["endAt"] != "2026-10-30T18:00:00+08:00" || len(r["passengers"].([]interface{})) != 2 {
		t.Errorf("reservation: %v", r)
	}
	if p["startAt"] != "2026-10-30T09:00:00+08:00" || p["endAt"] != "2026-10-30T17:00:00+08:00" {
		t.Errorf("previous: %v", p)
	}

	// A new reservation has no previous, and one made in MDM has no externalRef — both explicit nulls.
	created := BuildEvent("e", EventCreated, nil, res(func(r *domain.Reservation) { r.ExternalRef = nil }), "", "", now)
	raw, _ = json.Marshal(created)
	if !strings.Contains(string(raw), `"previous":null`) || !strings.Contains(string(raw), `"externalRef":null`) {
		t.Errorf("created event should carry null previous and externalRef: %s", raw)
	}
	if strings.Contains(string(raw), "changedBy") || strings.Contains(string(raw), `"note"`) {
		t.Errorf("empty changedBy/note are left out: %s", raw)
	}
}

func TestSignWebhook(t *testing.T) {
	// Computed independently with Python's hmac: HMAC-SHA256("s3cret", "1793266800.{\"a\":1}").
	got := SignWebhook("s3cret", "1793266800", []byte(`{"a":1}`))
	if got != "sha256=ec6181685d9e546db18cd0dff55434eb0aa443f756c7da62e32cf47656cd927b" {
		t.Fatal(got)
	}
	if SignWebhook("other", "1793266800", []byte(`{"a":1}`)) == got || SignWebhook("s3cret", "1793266801", []byte(`{"a":1}`)) == got ||
		SignWebhook("s3cret", "1793266800", []byte(`{"a":2}`)) == got {
		t.Fatal("secret, timestamp and body must all matter")
	}
}

func TestRetryDelay(t *testing.T) {
	want := []time.Duration{1, 2, 4, 8, 16}
	for i, w := range want {
		d, ok := RetryDelay(i + 1)
		if !ok || d != w*time.Minute {
			t.Errorf("after failure %d: %v %v, want %v", i+1, d, ok, w*time.Minute)
		}
	}
	for _, n := range []int{0, 6, 7} {
		if _, ok := RetryDelay(n); ok {
			t.Errorf("after failure %d: should give up", n)
		}
	}
}
