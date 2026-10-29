package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/adapter/postgres"
	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/service"
	"github.com/anthropics/mdm-server/internal/testutil/pgtest"
)

// These tests drive the integration API over HTTP against a real PostgreSQL
// (see pgtest): conflicts, locking and idempotency are SQL behaviour.

type apiEnv struct {
	t      *testing.T
	pool   *pgxpool.Pool
	srv    *httptest.Server
	repo   *postgres.IntegrationRepo
	key    string
	client string // integration client id
	asset  string
}

type apiResp struct {
	status int
	header http.Header
	raw    []byte
	json   map[string]interface{}
}

func (r apiResp) errCode() string {
	e, _ := r.json["error"].(map[string]interface{})
	code, _ := e["code"].(string)
	return code
}

func (r apiResp) str(field string) string {
	s, _ := r.json[field].(string)
	return s
}

func newAPIEnv(t *testing.T) *apiEnv {
	t.Helper()
	pool := pgtest.Pool(t)
	ctx := context.Background()
	repo := postgres.NewIntegrationRepo(pool)
	key, hash, prefix, err := middleware.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	client, err := repo.SetKey(ctx, "派發系統", hash, prefix)
	if err != nil {
		t.Fatal(err)
	}
	e := &apiEnv{t: t, pool: pool, repo: repo, key: key, client: client.ID}
	if err := pool.QueryRow(ctx,
		`INSERT INTO assets (asset_number, name, is_rentable) VALUES ('CAR-01','公務車 1',TRUE) RETURNING id`).Scan(&e.asset); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertResource(ctx, &domain.IntegrationResource{ResourceID: "RENTAL_CAR_01", ServiceType: "RENTAL_CAR", AssetID: e.asset,
		AllowedCities: []string{"台南市", "高雄市", "屏東縣"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	auth, err := middleware.NewIntegrationAuth(repo, []string{"127.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewReservationService(repo, postgres.NewReservationRepo(pool), postgres.NewUserRepo(pool))
	e.srv = httptest.NewServer(NewIntegrationController(svc, auth).Handler())
	t.Cleanup(e.srv.Close)
	return e
}

func (e *apiEnv) call(method, path string, body interface{}, headers map[string]string) apiResp {
	e.t.Helper()
	var rdr io.Reader
	if body != nil {
		var b []byte
		if s, ok := body.(string); ok {
			b = []byte(s)
		} else {
			b, _ = json.Marshal(body)
		}
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rdr)
	req.Header.Set("Authorization", "Bearer "+e.key)
	for k, v := range headers {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := apiResp{status: resp.StatusCode, header: resp.Header, raw: raw}
	json.Unmarshal(raw, &out.json)
	return out
}

var idemSeq atomic.Int64

// write is a call with a fresh Idempotency-Key.
func (e *apiEnv) write(method, path string, body interface{}) apiResp {
	return e.call(method, path, body, map[string]string{"Idempotency-Key": fmt.Sprintf("k-%d", idemSeq.Add(1))})
}

func (e *apiEnv) user(username, email, sso string, active bool) string {
	e.t.Helper()
	var id string
	if err := e.pool.QueryRow(context.Background(),
		`INSERT INTO users (username, password_hash, display_name, email, sso_sub, is_active) VALUES ($1,'x',$1,$2,NULLIF($3,''),$4) RETURNING id`,
		username, email, sso, active).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *apiEnv) count(sql string, args ...interface{}) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

const day = "2026-11-02" // a Monday

func at(date, hm string) string { return date + "T" + hm + ":00+08:00" }

func booking(ref, from, to string) map[string]interface{} {
	return map[string]interface{}{
		"externalRef": ref, "serviceType": "RENTAL_CAR",
		"borrowerEmployeeId": "wang.xm", "borrowerEmail": "wang.xm@mail.isha.org.tw",
		"passengers": []string{"wang.xm", "lee.xh"},
		"startAt":    at(day, from), "endAt": at(day, to),
		"destination": map[string]string{"address": "高雄市前鎮區○○路○號", "city": "高雄市", "district": "前鎮區"},
		"note":        "派工單 " + ref,
	}
}

func TestAPI_Auth(t *testing.T) {
	e := newAPIEnv(t)
	q := "/api/v1/availability?serviceType=RENTAL_CAR&from=" + at(day, "09:00") + "&to=" + at(day, "10:00")
	q = strings.ReplaceAll(q, "+", "%2B")

	if r := e.call("GET", q, nil, map[string]string{"Authorization": ""}); r.status != 401 || r.errCode() != "UNAUTHORIZED" {
		t.Fatalf("no key: %d %s", r.status, r.raw)
	}
	if r := e.call("GET", q, nil, map[string]string{"Authorization": "Bearer mdm_wrong"}); r.status != 401 {
		t.Fatalf("wrong key: %d", r.status)
	}
	if r := e.call("GET", q, nil, nil); r.status != 200 {
		t.Fatalf("good key: %d %s", r.status, r.raw)
	}

	off := false
	e.repo.UpdateClient(context.Background(), e.client, postgres.ClientUpdate{Enabled: &off})
	if r := e.call("GET", q, nil, nil); r.status != 403 || r.errCode() != "FORBIDDEN" {
		t.Fatalf("disabled key: %d %s", r.status, r.raw)
	}
	on := true
	elsewhere := []string{"203.0.113.0/24"}
	e.repo.UpdateClient(context.Background(), e.client, postgres.ClientUpdate{Enabled: &on, IPAllowlist: &elsewhere})
	if r := e.call("GET", q, nil, nil); r.status != 403 {
		t.Fatalf("ip outside allowlist: %d %s", r.status, r.raw)
	}
	// The peer here is loopback, a trusted proxy, so CF-Connecting-IP counts.
	if r := e.call("GET", q, nil, map[string]string{"CF-Connecting-IP": "203.0.113.9"}); r.status != 200 {
		t.Fatalf("ip inside allowlist via proxy header: %d %s", r.status, r.raw)
	}
	if r := e.call("GET", "/api/v1/nope", nil, nil); r.status != 404 {
		t.Fatalf("unknown path: %d", r.status)
	}
}

func TestAPI_CreateAndRead(t *testing.T) {
	e := newAPIEnv(t)
	e.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)

	r := e.write("POST", "/api/v1/reservations", booking("protrack:P-88", "09:00", "17:00"))
	if r.status != 201 {
		t.Fatalf("create: %d %s", r.status, r.raw)
	}
	if r.str("reservationId") == "" || !strings.HasPrefix(r.str("reservationId"), "R-") ||
		r.str("status") != "RESERVED" || r.str("resourceId") != "RENTAL_CAR_01" ||
		r.str("externalRef") != "protrack:P-88" || r.str("borrowerEmployeeId") != "wang.xm" ||
		r.str("startAt") != at(day, "09:00") || r.str("endAt") != at(day, "17:00") || r.str("createdVia") != "api" {
		t.Fatalf("unexpected body: %s", r.raw)
	}
	id := r.str("reservationId")

	// It is an approved rental of the car, timed to the minute.
	if n := e.count(`SELECT count(*) FROM rentals WHERE asset_id=$1 AND status='approved' AND created_via='api'
	                 AND start_at='2026-11-02 09:00+08' AND end_at='2026-11-02 17:00+08'
	                 AND (borrow_date AT TIME ZONE 'UTC')::date='2026-11-02' AND expected_return='2026-11-02'`, e.asset); n != 1 {
		t.Fatalf("rental row not as expected (%d)", n)
	}
	if n := e.count(`SELECT count(*) FROM audit_logs WHERE action='reservation.create' AND target=$1 AND user_id=$2`, id, "api:"+e.client); n != 1 {
		t.Fatalf("audit rows = %d", n)
	}

	g := e.call("GET", "/api/v1/reservations/"+id, nil, nil)
	if g.status != 200 || g.str("reservationId") != id || g.str("status") != "RESERVED" {
		t.Fatalf("get: %d %s", g.status, g.raw)
	}
	if p, _ := g.json["passengers"].([]interface{}); len(p) != 2 {
		t.Fatalf("passengers: %s", g.raw)
	}
	if d, _ := g.json["destination"].(map[string]interface{}); d["city"] != "高雄市" {
		t.Fatalf("destination: %s", g.raw)
	}
	if g := e.call("GET", "/api/v1/reservations/R-999999", nil, nil); g.status != 404 {
		t.Fatalf("unknown id: %d", g.status)
	}
	if g := e.call("GET", "/api/v1/reservations/garbage", nil, nil); g.status != 404 {
		t.Fatalf("malformed id: %d", g.status)
	}

	l := e.call("GET", "/api/v1/reservations?externalRef=protrack:P-88", nil, nil)
	if rs, _ := l.json["reservations"].([]interface{}); l.status != 200 || len(rs) != 1 {
		t.Fatalf("by externalRef: %d %s", l.status, l.raw)
	}
	if l := e.call("GET", "/api/v1/reservations?externalRef=protrack:none", nil, nil); l.status != 200 {
		t.Fatalf("unknown externalRef: %d", l.status)
	} else if rs, _ := l.json["reservations"].([]interface{}); len(rs) != 0 {
		t.Fatalf("unknown externalRef should list nothing: %s", l.raw)
	}
	if l := e.call("GET", "/api/v1/reservations", nil, nil); l.status != 400 {
		t.Fatalf("list without filter: %d", l.status)
	}
}

func TestAPI_ListByWindowIncludesWebRentalsAndCancelled(t *testing.T) {
	e := newAPIEnv(t)
	e.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)
	uid := e.user("web.user", "", "", true)

	a := e.write("POST", "/api/v1/reservations", booking("protrack:A", "09:00", "10:00"))
	b := e.write("POST", "/api/v1/reservations", booking("protrack:B", "10:00", "11:00"))
	if a.status != 201 || b.status != 201 {
		t.Fatalf("setup: %d %d", a.status, b.status)
	}
	e.write("POST", "/api/v1/reservations/"+b.str("reservationId")+"/cancel", map[string]string{"reason": "x"})
	// A rental made on the web UI the old way: a whole day, no times.
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO rentals (asset_id, borrower_id, borrower_name, status, period, borrow_date, expected_return, rental_number)
		 VALUES ($1,$2,'web','approved','full','2026-11-04'::timestamp AT TIME ZONE 'UTC','2026-11-04', 5000)`, e.asset, uid); err != nil {
		t.Fatal(err)
	}

	from, to := "2026-11-02T00:00:00%2B08:00", "2026-11-05T00:00:00%2B08:00"
	l := e.call("GET", "/api/v1/reservations?from="+from+"&to="+to+"&resourceId=RENTAL_CAR_01", nil, nil)
	rs, _ := l.json["reservations"].([]interface{})
	if l.status != 200 || len(rs) != 3 {
		t.Fatalf("window list: %d %s", l.status, l.raw)
	}
	byRef := map[string]map[string]interface{}{}
	for _, x := range rs {
		m := x.(map[string]interface{})
		ref, _ := m["externalRef"].(string)
		byRef[ref] = m
	}
	if byRef["protrack:B"]["status"] != "CANCELLED" || byRef["protrack:A"]["status"] != "RESERVED" {
		t.Fatalf("statuses: %s", l.raw)
	}
	web := byRef[""]
	if web["createdVia"] != "web" || web["startAt"] != "2026-11-04T00:00:00+08:00" || web["endAt"] != "2026-11-05T00:00:00+08:00" {
		t.Fatalf("web rental should show its whole-day span: %v", web)
	}
	// A window outside everything finds nothing.
	l = e.call("GET", "/api/v1/reservations?from=2026-12-01T00:00:00%2B08:00&to=2026-12-02T00:00:00%2B08:00", nil, nil)
	if rs, _ := l.json["reservations"].([]interface{}); len(rs) != 0 {
		t.Fatalf("empty window: %s", l.raw)
	}
}

func TestAPI_Validation(t *testing.T) {
	e := newAPIEnv(t)
	e.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)
	cases := map[string]func(m map[string]interface{}){
		"no externalRef":   func(m map[string]interface{}) { delete(m, "externalRef") },
		"no serviceType":   func(m map[string]interface{}) { delete(m, "serviceType") },
		"unknown service":  func(m map[string]interface{}) { m["serviceType"] = "TAXI" },
		"no start":         func(m map[string]interface{}) { delete(m, "startAt") },
		"start without tz": func(m map[string]interface{}) { m["startAt"] = "2026-11-02T09:00:00" },
		"seconds":          func(m map[string]interface{}) { m["startAt"] = "2026-11-02T09:00:30+08:00" },
		"end before start": func(m map[string]interface{}) { m["endAt"] = at(day, "08:00") },
		"end equals start": func(m map[string]interface{}) { m["endAt"] = at(day, "09:00") },
		"no borrower":      func(m map[string]interface{}) { delete(m, "borrowerEmployeeId"); delete(m, "borrowerEmail") },
		"bad email":        func(m map[string]interface{}) { m["borrowerEmail"] = "not-an-email" },
		"no destination":   func(m map[string]interface{}) { delete(m, "destination") },
		"too many riders":  func(m map[string]interface{}) { m["passengers"] = make([]string, 21) },
		"blank passenger":  func(m map[string]interface{}) { m["passengers"] = []string{"a", " "} },
		"unknown resource": func(m map[string]interface{}) { m["resourceId"] = "RENTAL_CAR_99" },
	}
	for name, mutate := range cases {
		m := booking("protrack:V-"+strings.ReplaceAll(name, " ", "-"), "09:00", "10:00")
		mutate(m)
		r := e.write("POST", "/api/v1/reservations", m)
		want := 400
		if name == "unknown resource" {
			want = 404
		}
		if r.status != want {
			t.Errorf("%s: status %d, want %d (%s)", name, r.status, want, r.raw)
		}
	}
	if r := e.write("POST", "/api/v1/reservations", "{not json"); r.status != 400 {
		t.Errorf("bad json: %d", r.status)
	}
	if r := e.write("POST", "/api/v1/reservations", ""); r.status != 400 {
		t.Errorf("empty body: %d", r.status)
	}
	if n := e.count(`SELECT count(*) FROM rentals`); n != 0 {
		t.Errorf("invalid requests must book nothing, found %d rentals", n)
	}
	// Writes need an Idempotency-Key.
	if r := e.call("POST", "/api/v1/reservations", booking("protrack:NK", "09:00", "10:00"), nil); r.status != 400 {
		t.Errorf("missing Idempotency-Key: %d %s", r.status, r.raw)
	}
}

func TestAPI_RegionCheck(t *testing.T) {
	e := newAPIEnv(t)
	e.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)
	for i, tc := range []struct {
		city string
		want int
	}{{"台北市", 422}, {"新北市", 422}, {"臺南市", 201}, {"高雄市", 201}, {" 屏東縣 ", 201}} {
		m := booking(fmt.Sprintf("protrack:R-%d", i), fmt.Sprintf("%02d:00", 8+i), fmt.Sprintf("%02d:30", 8+i))
		m["destination"] = map[string]string{"city": tc.city}
		r := e.write("POST", "/api/v1/reservations", m)
		if r.status != tc.want {
			t.Errorf("%q: status %d, want %d (%s)", tc.city, r.status, tc.want, r.raw)
		}
		if tc.want == 422 && r.errCode() != "REGION_NOT_SUPPORTED" {
			t.Errorf("%q: code %s", tc.city, r.errCode())
		}
	}
}

func TestAPI_ConflictsAndAvailability(t *testing.T) {
	e := newAPIEnv(t)
	e.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)
	avail := func(from, to string) apiResp {
		return e.call("GET", "/api/v1/availability?serviceType=RENTAL_CAR&from="+strings.ReplaceAll(at(day, from), "+", "%2B")+
			"&to="+strings.ReplaceAll(at(day, to), "+", "%2B"), nil, nil)
	}
	if r := avail("09:00", "12:00"); r.status != 200 || r.json["available"] != true || r.str("resourceId") != "RENTAL_CAR_01" {
		t.Fatalf("empty calendar: %d %s", r.status, r.raw)
	}
	if r := e.write("POST", "/api/v1/reservations", booking("protrack:1", "09:00", "12:00")); r.status != 201 {
		t.Fatalf("first: %d %s", r.status, r.raw)
	}

	// Overlaps in every way: 409 VEHICLE_UNAVAILABLE naming the reservation in the way.
	for _, w := range [][2]string{{"09:00", "12:00"}, {"08:00", "09:01"}, {"11:59", "13:00"}, {"10:00", "11:00"}, {"08:00", "18:00"}} {
		r := e.write("POST", "/api/v1/reservations", booking("protrack:x", w[0], w[1]))
		if r.status != 409 || r.errCode() != "VEHICLE_UNAVAILABLE" {
			t.Errorf("%v should conflict: %d %s", w, r.status, r.raw)
		}
		if d, _ := r.json["error"].(map[string]interface{})["details"].([]interface{}); len(d) != 1 {
			t.Errorf("%v: details should list the conflict: %s", w, r.raw)
		}
		if a := avail(w[0], w[1]); a.json["available"] != false || len(a.json["conflicts"].([]interface{})) != 1 {
			t.Errorf("%v: availability should report the conflict: %s", w, a.raw)
		}
	}
	// Touching at the minute is fine: [start, end).
	for i, w := range [][2]string{{"07:00", "09:00"}, {"12:00", "13:00"}} {
		if a := avail(w[0], w[1]); a.json["available"] != true {
			t.Errorf("%v should be free: %s", w, a.raw)
		}
		if r := e.write("POST", "/api/v1/reservations", booking(fmt.Sprintf("protrack:adj-%d", i), w[0], w[1])); r.status != 201 {
			t.Errorf("%v back-to-back: %d %s", w, r.status, r.raw)
		}
	}
}

func TestAPI_CarOutOfServiceIsUnavailable(t *testing.T) {
	e := newAPIEnv(t)
	e.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)
	e.pool.Exec(context.Background(), `UPDATE assets SET asset_status='maintenance' WHERE id=$1`, e.asset)
	r := e.write("POST", "/api/v1/reservations", booking("protrack:m", "09:00", "10:00"))
	if r.status != 409 || r.errCode() != "VEHICLE_UNAVAILABLE" {
		t.Fatalf("in maintenance: %d %s", r.status, r.raw)
	}
	a := e.call("GET", "/api/v1/availability?serviceType=RENTAL_CAR&from=2026-11-02T09:00:00%2B08:00&to=2026-11-02T10:00:00%2B08:00", nil, nil)
	if a.json["available"] != false || a.str("reason") != "ASSET_MAINTENANCE" {
		t.Fatalf("availability: %s", a.raw)
	}
	// 'rented' only means it is out right now: later spans can still be booked.
	e.pool.Exec(context.Background(), `UPDATE assets SET asset_status='rented' WHERE id=$1`, e.asset)
	if r := e.write("POST", "/api/v1/reservations", booking("protrack:m2", "09:00", "10:00")); r.status != 201 {
		t.Fatalf("rented asset, free span: %d %s", r.status, r.raw)
	}
}

func TestAPI_IdempotencyKey(t *testing.T) {
	e := newAPIEnv(t)
	e.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)
	hdr := map[string]string{"Idempotency-Key": "same-key"}
	body := booking("protrack:I-1", "09:00", "10:00")

	first := e.call("POST", "/api/v1/reservations", body, hdr)
	if first.status != 201 {
		t.Fatalf("first: %d %s", first.status, first.raw)
	}
	// Same request (even with the JSON laid out differently): same answer, nothing new booked.
	reordered := `{"note":"派工單 protrack:I-1","endAt":"` + at(day, "10:00") + `","startAt":"` + at(day, "09:00") + `",
	  "destination":{"district":"前鎮區","city":"高雄市","address":"高雄市前鎮區○○路○號"},"passengers":["wang.xm","lee.xh"],
	  "borrowerEmail":"wang.xm@mail.isha.org.tw","borrowerEmployeeId":"wang.xm","serviceType":"RENTAL_CAR","externalRef":"protrack:I-1"}`
	again := e.call("POST", "/api/v1/reservations", reordered, hdr)
	if again.status != 201 || !bytes.Equal(again.raw, first.raw) || again.header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: %d replayed=%q\n%s\nvs\n%s", again.status, again.header.Get("Idempotent-Replayed"), again.raw, first.raw)
	}
	if n := e.count(`SELECT count(*) FROM rentals WHERE external_ref='protrack:I-1'`); n != 1 {
		t.Fatalf("rentals = %d", n)
	}
	// The same key for another request is refused.
	other := e.call("POST", "/api/v1/reservations", booking("protrack:I-2", "11:00", "12:00"), hdr)
	if other.status != 409 || other.errCode() != "IDEMPOTENCY_KEY_CONFLICT" {
		t.Fatalf("key reuse: %d %s", other.status, other.raw)
	}
	// Even after the reservation was cancelled, the retry still gets the original answer.
	e.write("POST", "/api/v1/reservations/"+first.str("reservationId")+"/cancel", nil)
	if again := e.call("POST", "/api/v1/reservations", body, hdr); !bytes.Equal(again.raw, first.raw) {
		t.Fatalf("replay after cancel changed: %s", again.raw)
	}
}

func TestAPI_FailedWritesAreNotRemembered(t *testing.T) {
	e := newAPIEnv(t)
	e.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)
	blocker := e.write("POST", "/api/v1/reservations", booking("protrack:blocker", "09:00", "10:00"))
	hdr := map[string]string{"Idempotency-Key": "retry-me"}
	body := booking("protrack:late", "09:30", "10:30")
	if r := e.call("POST", "/api/v1/reservations", body, hdr); r.status != 409 {
		t.Fatalf("should conflict: %d %s", r.status, r.raw)
	}
	e.write("POST", "/api/v1/reservations/"+blocker.str("reservationId")+"/cancel", nil)
	if r := e.call("POST", "/api/v1/reservations", body, hdr); r.status != 201 {
		t.Fatalf("retry after the car freed up: %d %s", r.status, r.raw)
	}
}

func TestAPI_ExternalRefIsUniqueWhileLive(t *testing.T) {
	e := newAPIEnv(t)
	e.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)
	a := e.write("POST", "/api/v1/reservations", booking("protrack:P-1", "09:00", "10:00"))
	dup := e.write("POST", "/api/v1/reservations", booking("protrack:P-1", "11:00", "12:00"))
	if dup.status != 409 || dup.errCode() != "EXTERNAL_REF_EXISTS" {
		t.Fatalf("duplicate: %d %s", dup.status, dup.raw)
	}
	if d := dup.json["error"].(map[string]interface{})["details"].(map[string]interface{}); d["reservationId"] != a.str("reservationId") {
		t.Fatalf("details should point at the existing reservation: %s", dup.raw)
	}
	e.write("POST", "/api/v1/reservations/"+a.str("reservationId")+"/cancel", nil)
	if r := e.write("POST", "/api/v1/reservations", booking("protrack:P-1", "11:00", "12:00")); r.status != 201 {
		t.Fatalf("re-create after cancel: %d %s", r.status, r.raw)
	}
	l := e.call("GET", "/api/v1/reservations?externalRef=protrack:P-1", nil, nil)
	rs, _ := l.json["reservations"].([]interface{})
	if len(rs) != 2 || rs[0].(map[string]interface{})["status"] != "RESERVED" {
		t.Fatalf("live reservation should list first: %s", l.raw)
	}
}

func TestAPI_ConcurrentBookingsOfTheSameSlot(t *testing.T) {
	e := newAPIEnv(t)
	e.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)
	const n = 8
	var wg sync.WaitGroup
	statuses := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses[i] = e.write("POST", "/api/v1/reservations", booking(fmt.Sprintf("protrack:C-%d", i), "09:00", "10:00")).status
		}(i)
	}
	wg.Wait()
	created, conflicts := 0, 0
	for _, s := range statuses {
		switch s {
		case 201:
			created++
		case 409:
			conflicts++
		}
	}
	if created != 1 || conflicts != n-1 {
		t.Fatalf("statuses %v: want exactly one 201", statuses)
	}
	if c := e.count(`SELECT count(*) FROM rentals`); c != 1 {
		t.Fatalf("rentals = %d", c)
	}
}

func TestAPI_ConcurrentNonOverlappingBookingsGetDistinctNumbers(t *testing.T) {
	e := newAPIEnv(t)
	e.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)
	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := e.write("POST", "/api/v1/reservations", booking(fmt.Sprintf("protrack:D-%d", i), fmt.Sprintf("%02d:00", 6+i), fmt.Sprintf("%02d:30", 6+i)))
			if r.status != 201 {
				t.Errorf("booking %d: %d %s", i, r.status, r.raw)
			}
		}(i)
	}
	wg.Wait()
	if c := e.count(`SELECT count(DISTINCT rental_number) FROM rentals`); c != n {
		t.Fatalf("distinct rental numbers = %d, want %d", c, n)
	}
}

// Different cars do not lock each other, so the rental number counter has its own lock.
func TestAPI_ConcurrentBookingsOfDifferentCarsGetDistinctNumbers(t *testing.T) {
	e := newAPIEnv(t)
	e.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)
	var second string
	e.pool.QueryRow(context.Background(),
		`INSERT INTO assets (asset_number, name, is_rentable) VALUES ('CAR-02','公務車 2',TRUE) RETURNING id`).Scan(&second)
	e.repo.UpsertResource(context.Background(), &domain.IntegrationResource{ResourceID: "RENTAL_CAR_02", ServiceType: "RENTAL_CAR",
		AssetID: second, AllowedCities: []string{"高雄市"}, Enabled: true})
	const n = 16
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m := booking(fmt.Sprintf("protrack:X-%d", i), fmt.Sprintf("%02d:00", 6+i/2), fmt.Sprintf("%02d:30", 6+i/2))
			m["resourceId"] = fmt.Sprintf("RENTAL_CAR_0%d", 1+i%2)
			if r := e.write("POST", "/api/v1/reservations", m); r.status != 201 {
				t.Errorf("booking %d: %d %s", i, r.status, r.raw)
			}
		}(i)
	}
	wg.Wait()
	if c := e.count(`SELECT count(DISTINCT rental_number) FROM rentals`); c != n {
		t.Fatalf("distinct rental numbers = %d, want %d", c, n)
	}
}

func TestAPI_UpdateReservation(t *testing.T) {
	e := newAPIEnv(t)
	e.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)
	e.user("lee.xh", "lee.xh@mail.isha.org.tw", "sub-lee", true)
	a := e.write("POST", "/api/v1/reservations", booking("protrack:U-1", "09:00", "12:00"))
	b := e.write("POST", "/api/v1/reservations", booking("protrack:U-2", "13:00", "15:00"))
	id := a.str("reservationId")

	// Move it later: fine, even over its own old span.
	r := e.write("PUT", "/api/v1/reservations/"+id, map[string]string{"startAt": at(day, "10:00"), "endAt": at(day, "12:30")})
	if r.status != 200 || r.str("startAt") != at(day, "10:00") || r.str("endAt") != at(day, "12:30") || r.str("externalRef") != "protrack:U-1" {
		t.Fatalf("reschedule: %d %s", r.status, r.raw)
	}
	// Into the other reservation: refused, unchanged.
	r = e.write("PUT", "/api/v1/reservations/"+id, map[string]string{"endAt": at(day, "13:01")})
	if r.status != 409 || r.errCode() != "VEHICLE_UNAVAILABLE" {
		t.Fatalf("overlap: %d %s", r.status, r.raw)
	}
	if g := e.call("GET", "/api/v1/reservations/"+id, nil, nil); g.str("endAt") != at(day, "12:30") {
		t.Fatalf("a refused change must not apply: %s", g.raw)
	}
	// Up to the other one's start is fine.
	if r := e.write("PUT", "/api/v1/reservations/"+id, map[string]string{"endAt": at(day, "13:00")}); r.status != 200 {
		t.Fatalf("touching: %d %s", r.status, r.raw)
	}
	// Passengers, note and borrower.
	r = e.write("PUT", "/api/v1/reservations/"+id, map[string]interface{}{
		"passengers": []string{"lee.xh"}, "note": "改人", "borrowerEmployeeId": "lee.xh", "borrowerEmail": "lee.xh@mail.isha.org.tw"})
	if r.status != 200 || r.str("note") != "改人" || r.str("borrowerEmployeeId") != "lee.xh" || r.str("borrowerEmail") != "lee.xh@mail.isha.org.tw" {
		t.Fatalf("details: %d %s", r.status, r.raw)
	}
	if p := r.json["passengers"].([]interface{}); len(p) != 1 || p[0] != "lee.xh" {
		t.Fatalf("passengers: %s", r.raw)
	}
	// Destination is re-checked.
	if r := e.write("PUT", "/api/v1/reservations/"+id, map[string]interface{}{"destination": map[string]string{"city": "台北市"}}); r.status != 422 {
		t.Fatalf("region on update: %d %s", r.status, r.raw)
	}
	// Things that cannot change, nothing to change, unknown id.
	for name, body := range map[string]interface{}{
		"externalRef": map[string]string{"externalRef": "protrack:other"},
		"resourceId":  map[string]string{"resourceId": "RENTAL_CAR_02"},
		"empty":       map[string]string{},
		"bad time":    map[string]string{"startAt": "soon"},
		"end<start":   map[string]string{"endAt": at(day, "09:00")},
	} {
		if r := e.write("PUT", "/api/v1/reservations/"+id, body); r.status != 400 {
			t.Errorf("%s: %d %s", name, r.status, r.raw)
		}
	}
	if r := e.write("PUT", "/api/v1/reservations/R-424242", map[string]string{"note": "x"}); r.status != 404 {
		t.Errorf("unknown: %d", r.status)
	}
	// Same externalRef echoed back is allowed (a client may send the whole object).
	if r := e.write("PUT", "/api/v1/reservations/"+id, map[string]string{"externalRef": "protrack:U-1", "note": "n2"}); r.status != 200 {
		t.Errorf("echoed externalRef: %d %s", r.status, r.raw)
	}
	// Not once it is out or cancelled.
	e.pool.Exec(context.Background(), `UPDATE rentals SET status='active' WHERE external_ref='protrack:U-2'`)
	if r := e.write("PUT", "/api/v1/reservations/"+b.str("reservationId"), map[string]string{"note": "x"}); r.status != 422 || r.errCode() != "RESERVATION_NOT_MODIFIABLE" {
		t.Errorf("in use: %d %s", r.status, r.raw)
	}
	e.write("POST", "/api/v1/reservations/"+id+"/cancel", nil)
	if r := e.write("PUT", "/api/v1/reservations/"+id, map[string]string{"note": "x"}); r.status != 422 {
		t.Errorf("cancelled: %d %s", r.status, r.raw)
	}
}

func TestAPI_CancelReservation(t *testing.T) {
	e := newAPIEnv(t)
	e.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)
	a := e.write("POST", "/api/v1/reservations", booking("protrack:K-1", "09:00", "10:00"))
	id := a.str("reservationId")

	r := e.write("POST", "/api/v1/reservations/"+id+"/cancel", map[string]string{"reason": "出差取消"})
	if r.status != 200 || r.str("status") != "CANCELLED" || r.str("cancelReason") != "出差取消" {
		t.Fatalf("cancel: %d %s", r.status, r.raw)
	}
	// It is a status change, not a delete; the slot is free again.
	if n := e.count(`SELECT count(*) FROM rentals WHERE status='cancelled' AND cancelled_at IS NOT NULL`); n != 1 {
		t.Fatalf("row should be kept as cancelled (%d)", n)
	}
	if g := e.call("GET", "/api/v1/reservations/"+id, nil, nil); g.str("status") != "CANCELLED" {
		t.Fatalf("get after cancel: %s", g.raw)
	}
	if r := e.write("POST", "/api/v1/reservations", booking("protrack:K-2", "09:00", "10:00")); r.status != 201 {
		t.Fatalf("slot should be free: %d %s", r.status, r.raw)
	}
	// Cancelling again (new key) is not an error and changes nothing.
	r = e.write("POST", "/api/v1/reservations/"+id+"/cancel", map[string]string{"reason": "again"})
	if r.status != 200 || r.str("cancelReason") != "出差取消" {
		t.Fatalf("second cancel: %d %s", r.status, r.raw)
	}
	// Once the car is out, or back, it cannot be cancelled.
	for _, st := range []string{"active", "pending_return", "returned"} {
		x := e.write("POST", "/api/v1/reservations", booking("protrack:K-"+st, "14:00", "15:00"))
		e.pool.Exec(context.Background(), `UPDATE rentals SET status=$1 WHERE external_ref=$2`, st, "protrack:K-"+st)
		if r := e.write("POST", "/api/v1/reservations/"+x.str("reservationId")+"/cancel", nil); r.status != 422 {
			t.Errorf("cancel while %s: %d %s", st, r.status, r.raw)
		}
		e.pool.Exec(context.Background(), `UPDATE rentals SET status='cancelled' WHERE external_ref=$1`, "protrack:K-"+st)
	}
	if r := e.write("POST", "/api/v1/reservations/R-424242/cancel", nil); r.status != 404 {
		t.Errorf("unknown: %d", r.status)
	}
	// A rejected rental (web) shows as cancelled.
	e.pool.Exec(context.Background(), `UPDATE rentals SET status='rejected' WHERE external_ref='protrack:K-2'`)
	if g := e.call("GET", "/api/v1/reservations?externalRef=protrack:K-2", nil, nil); !strings.Contains(string(g.raw), `"status":"CANCELLED"`) {
		t.Errorf("rejected should read as CANCELLED: %s", g.raw)
	}
}

func TestAPI_StatusMappingFollowsTheRental(t *testing.T) {
	e := newAPIEnv(t)
	e.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)
	x := e.write("POST", "/api/v1/reservations", booking("protrack:S", "09:00", "10:00"))
	for rental, want := range map[string]string{"pending": "RESERVED", "approved": "RESERVED", "active": "IN_USE", "pending_return": "IN_USE", "returned": "RETURNED"} {
		e.pool.Exec(context.Background(), `UPDATE rentals SET status=$1 WHERE external_ref='protrack:S'`, rental)
		if g := e.call("GET", "/api/v1/reservations/"+x.str("reservationId"), nil, nil); g.str("status") != want {
			t.Errorf("%s -> %s, want %s", rental, g.str("status"), want)
		}
	}
}

func TestAPI_Borrowers(t *testing.T) {
	e := newAPIEnv(t)
	e.user("sso.user", "sso.user@mail.isha.org.tw", "sub-1", true)
	e.user("local.user", "", "", true)
	e.user("gone", "gone@mail.isha.org.tw", "sub-g", false)
	e.user("twin1", "twin@mail.isha.org.tw", "", true)
	e.user("twin2", "Twin@mail.isha.org.tw", "", true)
	n := 0
	book := func(m map[string]interface{}) apiResp {
		n++
		m["externalRef"] = fmt.Sprintf("protrack:B-%d", n)
		m["serviceType"] = "RENTAL_CAR"
		m["startAt"], m["endAt"] = at(day, fmt.Sprintf("%02d:00", 5+n)), at(day, fmt.Sprintf("%02d:30", 5+n))
		m["destination"] = map[string]string{"city": "高雄市"}
		return e.write("POST", "/api/v1/reservations", m)
	}

	if r := book(map[string]interface{}{"borrowerEmployeeId": "sso.user"}); r.status != 201 || r.str("borrowerEmail") != "sso.user@mail.isha.org.tw" {
		t.Errorf("SSO-linked account by employee id alone: %d %s", r.status, r.raw)
	}
	if r := book(map[string]interface{}{"borrowerEmail": "SSO.User@Mail.Isha.org.tw"}); r.status != 201 || r.str("borrowerEmployeeId") != "sso.user" {
		t.Errorf("email matches without regard to case, and the SSO username is the employee id: %d %s", r.status, r.raw)
	}
	// A local account's username means nothing outside MDM: not matched by employee id.
	if r := book(map[string]interface{}{"borrowerEmployeeId": "local.user"}); r.status != 422 || r.errCode() != "BORROWER_NOT_FOUND" {
		t.Errorf("local account by employee id: %d %s", r.status, r.raw)
	}
	if r := book(map[string]interface{}{"borrowerEmployeeId": "nobody"}); r.status != 422 || r.errCode() != "BORROWER_NOT_FOUND" {
		t.Errorf("unknown, no email: %d %s", r.status, r.raw)
	}
	if r := book(map[string]interface{}{"borrowerEmployeeId": "gone"}); r.status != 422 || r.errCode() != "BORROWER_INACTIVE" {
		t.Errorf("inactive: %d %s", r.status, r.raw)
	}
	if r := book(map[string]interface{}{"borrowerEmail": "twin@mail.isha.org.tw"}); r.status != 422 || r.errCode() != "BORROWER_AMBIGUOUS" {
		t.Errorf("two accounts with one email: %d %s", r.status, r.raw)
	}
	// An email-shaped employee id is taken as the email.
	if r := book(map[string]interface{}{"borrowerEmployeeId": "sso.user@mail.isha.org.tw"}); r.status != 201 {
		t.Errorf("email as id: %d %s", r.status, r.raw)
	}
	// An SSO-linked username with a different email is somebody else.
	if r := book(map[string]interface{}{"borrowerEmployeeId": "sso.user", "borrowerEmail": "other@mail.isha.org.tw"}); r.status != 201 {
		t.Errorf("same username, other email: %d %s", r.status, r.raw)
	} else if r.str("borrowerEmail") != "other@mail.isha.org.tw" {
		t.Errorf("should have been a new account for the other email: %s", r.raw)
	}
}

func TestAPI_BorrowerAccountIsCreatedForTheOrganisationDomain(t *testing.T) {
	e := newAPIEnv(t)
	body := func(ref, from string) map[string]interface{} {
		m := booking(ref, from, from[:2]+":30")
		m["borrowerEmployeeId"], m["borrowerEmail"], m["borrowerName"] = "new.person", "New.Person@Mail.Isha.org.tw", "新同事"
		return m
	}
	r := e.write("POST", "/api/v1/reservations", body("protrack:N-1", "09:00"))
	if r.status != 201 || r.str("borrowerEmployeeId") != "new.person" || r.str("borrowerName") != "新同事" || r.str("borrowerEmail") != "new.person@mail.isha.org.tw" {
		t.Fatalf("create: %d %s", r.status, r.raw)
	}
	if n := e.count(`SELECT count(*) FROM users WHERE username='new.person' AND email='new.person@mail.isha.org.tw'
	                 AND display_name='新同事' AND password_hash='' AND sso_sub IS NULL AND is_active`); n != 1 {
		t.Fatalf("account not created as expected (%d)", n)
	}
	// The next reservation finds the same account instead of making another.
	if r := e.write("POST", "/api/v1/reservations", body("protrack:N-2", "10:00")); r.status != 201 {
		t.Fatalf("second: %d %s", r.status, r.raw)
	}
	if n := e.count(`SELECT count(*) FROM users WHERE lower(email)='new.person@mail.isha.org.tw'`); n != 1 {
		t.Fatalf("accounts = %d", n)
	}
	// SSO sign-in later finds it by email (the app's lookup is case-insensitive).
	u, err := postgres.NewUserRepo(e.pool).GetByEmail(context.Background(), "New.Person@mail.isha.org.tw")
	if err != nil || u.Username != "new.person" {
		t.Fatalf("SSO lookup: %v %v", u, err)
	}
	// A taken username gets a suffix rather than failing the booking.
	e.user("taken", "", "", true)
	m := body("protrack:N-3", "11:00")
	m["borrowerEmployeeId"], m["borrowerEmail"] = "taken", "taken.person@mail.isha.org.tw"
	if r := e.write("POST", "/api/v1/reservations", m); r.status != 201 {
		t.Fatalf("taken username: %d %s", r.status, r.raw)
	}
	if n := e.count(`SELECT count(*) FROM users WHERE email='taken.person@mail.isha.org.tw' AND username LIKE 'taken\_%'`); n != 1 {
		t.Fatalf("expected a suffixed username")
	}
	// Other domains are not created for.
	m = body("protrack:N-4", "12:00")
	m["borrowerEmployeeId"], m["borrowerEmail"] = "outsider", "outsider@example.com"
	if r := e.write("POST", "/api/v1/reservations", m); r.status != 422 || r.errCode() != "BORROWER_NOT_FOUND" {
		t.Fatalf("other domain: %d %s", r.status, r.raw)
	}
	// Switch creation off, and a newcomer is refused.
	none := []string{}
	e.repo.UpdateClient(context.Background(), e.client, postgres.ClientUpdate{AutoCreateEmailDomains: &none})
	m = body("protrack:N-5", "13:00")
	m["borrowerEmployeeId"], m["borrowerEmail"] = "another", "another@mail.isha.org.tw"
	if r := e.write("POST", "/api/v1/reservations", m); r.status != 422 {
		t.Fatalf("creation switched off: %d %s", r.status, r.raw)
	}
	// ... and no account is left behind when a booking fails.
	if n := e.count(`SELECT count(*) FROM users WHERE username IN ('another','outsider')`); n != 0 {
		t.Fatalf("refused requests left %d accounts", n)
	}
}

func TestAPI_NoAccountIsLeftWhenTheBookingConflicts(t *testing.T) {
	e := newAPIEnv(t)
	e.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)
	e.write("POST", "/api/v1/reservations", booking("protrack:first", "09:00", "10:00"))
	m := booking("protrack:second", "09:30", "10:30")
	m["borrowerEmployeeId"], m["borrowerEmail"] = "late.comer", "late.comer@mail.isha.org.tw"
	if r := e.write("POST", "/api/v1/reservations", m); r.status != 409 {
		t.Fatalf("should conflict: %d %s", r.status, r.raw)
	}
	if n := e.count(`SELECT count(*) FROM users WHERE username='late.comer'`); n != 0 {
		t.Fatalf("a failed booking created %d accounts", n)
	}
}

func TestAPI_SeveralResourcesOfOneServiceType(t *testing.T) {
	e := newAPIEnv(t)
	e.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)
	var second string
	e.pool.QueryRow(context.Background(),
		`INSERT INTO assets (asset_number, name, is_rentable) VALUES ('CAR-02','公務車 2',TRUE) RETURNING id`).Scan(&second)
	if err := e.repo.UpsertResource(context.Background(), &domain.IntegrationResource{ResourceID: "RENTAL_CAR_02", ServiceType: "RENTAL_CAR", AssetID: second,
		AllowedCities: []string{"高雄市"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	a := e.write("POST", "/api/v1/reservations", booking("protrack:M-1", "09:00", "10:00"))
	b := e.write("POST", "/api/v1/reservations", booking("protrack:M-2", "09:00", "10:00"))
	if a.str("resourceId") != "RENTAL_CAR_01" || b.str("resourceId") != "RENTAL_CAR_02" {
		t.Fatalf("second request should take the second car: %s / %s", a.raw, b.raw)
	}
	if c := e.write("POST", "/api/v1/reservations", booking("protrack:M-3", "09:00", "10:00")); c.status != 409 {
		t.Fatalf("both busy: %d %s", c.status, c.raw)
	}
	av := e.call("GET", "/api/v1/availability?serviceType=RENTAL_CAR&from=2026-11-02T09:00:00%2B08:00&to=2026-11-02T10:00:00%2B08:00", nil, nil)
	if av.json["available"] != false {
		t.Fatalf("both busy, availability: %s", av.raw)
	}
	// A named resource is used even when another is free; and its own region list applies.
	named := booking("protrack:M-4", "11:00", "12:00")
	named["resourceId"] = "RENTAL_CAR_02"
	named["destination"] = map[string]string{"city": "台南市"}
	if r := e.write("POST", "/api/v1/reservations", named); r.status != 422 {
		t.Fatalf("台南市 is not allowed for car 2: %d %s", r.status, r.raw)
	}
	// Without naming one, the car whose region fits is chosen.
	open := booking("protrack:M-5", "11:00", "12:00")
	open["destination"] = map[string]string{"city": "台南市"}
	if r := e.write("POST", "/api/v1/reservations", open); r.status != 201 || r.str("resourceId") != "RENTAL_CAR_01" {
		t.Fatalf("台南市 fits only car 1: %d %s", r.status, r.raw)
	}
	e.repo.UpsertResource(context.Background(), &domain.IntegrationResource{ResourceID: "RENTAL_CAR_02", ServiceType: "RENTAL_CAR", AssetID: second, Enabled: false})
	if c := e.write("POST", "/api/v1/reservations", booking("protrack:M-6", "09:00", "10:00")); c.status != 409 {
		t.Fatalf("disabled car must not be booked: %d %s", c.status, c.raw)
	}
}
