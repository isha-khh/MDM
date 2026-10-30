package controller

import "testing"

// The rentals page shows a reservation made by the dispatch system with its
// exact time span, reference, riders and destination.
func TestRentalsList_ShowsDispatchBookingDetails(t *testing.T) {
	e := newWebEnv(t)
	e.apiEnv.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)
	if r := e.write("POST", "/api/v1/reservations", booking("protrack:P-88", "09:16", "17:16")); r.status != 201 {
		t.Fatalf("api booking: %d %s", r.status, r.raw)
	}
	e.rentCar(e.asset2()) // an ordinary whole-day rental made on the web

	st, out := e.webCall("GET", "/api/rentals", nil)
	if st != 200 {
		t.Fatalf("list: %d", st)
	}
	var api, web map[string]interface{}
	for _, x := range out["rentals"].([]interface{}) {
		row := x.(map[string]interface{})
		if row["created_via"] == "api" {
			api = row
		} else {
			web = row
		}
	}
	if api == nil || web == nil {
		t.Fatalf("expected one api and one web rental: %v", out)
	}
	if api["external_ref"] != "protrack:P-88" || api["start_at"] != "2026-11-02T09:16:00+08:00" || api["end_at"] != "2026-11-02T17:16:00+08:00" ||
		api["borrower_email"] != "wang.xm@mail.isha.org.tw" || api["status"] != "approved" {
		t.Fatalf("api rental: %v", api)
	}
	if p := api["passengers"].([]interface{}); len(p) != 2 || p[0] != "wang.xm" {
		t.Fatalf("passengers: %v", api["passengers"])
	}
	if d := api["destination"].(map[string]interface{}); d["city"] != "高雄市" || d["district"] != "前鎮區" {
		t.Fatalf("destination: %v", api["destination"])
	}
	if web["external_ref"] != nil || web["start_at"] != nil || web["end_at"] != nil || web["destination"] != nil || web["borrower_email"] != "borrower.b@mail.isha.org.tw" {
		t.Fatalf("web rental: %v", web)
	}
	if p := web["passengers"].([]interface{}); len(p) != 0 {
		t.Fatalf("web rental passengers: %v", p)
	}
}
