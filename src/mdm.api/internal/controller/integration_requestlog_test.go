package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/mdm-server/internal/adapter/postgres"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/testutil/pgtest"
)

func TestRequestLog_RingKeepsTheNewestAndStripsBodiesFromTheList(t *testing.T) {
	l := NewRequestLog(3)
	if got := l.Recent(10); len(got) != 0 {
		t.Fatalf("empty log: %v", got)
	}
	for i := 1; i <= 5; i++ {
		l.Add(RequestRecord{Path: fmt.Sprint("/p", i), RequestBody: "in", ResponseBody: "out"})
	}
	got := l.Recent(10)
	if len(got) != 3 || got[0].Path != "/p5" || got[1].Path != "/p4" || got[2].Path != "/p3" {
		t.Fatalf("newest first, only the last three: %+v", got)
	}
	if got[0].ID != 5 || got[2].ID != 3 {
		t.Fatalf("ids: %d %d", got[0].ID, got[2].ID)
	}
	if got[0].RequestBody != "" || got[0].ResponseBody != "" {
		t.Fatal("the list must not carry bodies")
	}
	if two := l.Recent(2); len(two) != 2 || two[0].Path != "/p5" {
		t.Fatalf("limit: %+v", two)
	}
	if r, ok := l.Get(4); !ok || r.RequestBody != "in" || r.ResponseBody != "out" {
		t.Fatalf("detail: %+v %v", r, ok)
	}
	if _, ok := l.Get(1); ok {
		t.Fatal("record 1 was pushed out")
	}
	var nilLog *RequestLog
	nilLog.Add(RequestRecord{})
	if len(nilLog.Recent(5)) != 0 {
		t.Fatal("a nil log is empty")
	}
	if _, ok := nilLog.Get(1); ok {
		t.Fatal("a nil log has nothing")
	}
}

func TestRequestLog_CaptureStopsAtTheCap(t *testing.T) {
	var c capture
	c.add(bytes.Repeat([]byte("a"), maxLoggedBody-1))
	if c.truncated || c.buf.Len() != maxLoggedBody-1 {
		t.Fatal("under the cap")
	}
	c.add([]byte("bcd"))
	if !c.truncated || c.buf.Len() != maxLoggedBody {
		t.Fatalf("over the cap: %d truncated=%v", c.buf.Len(), c.truncated)
	}
	c.add([]byte("e"))
	if c.buf.Len() != maxLoggedBody {
		t.Fatal("nothing more is kept")
	}
}

func lastRecord(t *testing.T, e *apiEnv) RequestRecord {
	t.Helper()
	r := e.reqLog.Recent(1)
	if len(r) != 1 {
		t.Fatal("no request was logged")
	}
	full, _ := e.reqLog.Get(r[0].ID)
	return full
}

func TestRequestLog_RecordsEveryKindOfCall(t *testing.T) {
	var out bytes.Buffer
	log.SetOutput(&out)
	defer log.SetOutput(log.Writer())
	e := newAPIEnv(t)
	e.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)

	// Not signed in at all.
	e.call("GET", "/api/v1/reservations?externalRef=x", nil, map[string]string{"Authorization": ""})
	if r := lastRecord(t, e); r.Status != 401 || r.ErrorCode != "UNAUTHORIZED" || r.Client != "" || r.IP != "127.0.0.1" || r.KeyHint != "" ||
		r.Method != "GET" || r.Path != "/api/v1/reservations" || r.Query != "externalRef=x" {
		t.Fatalf("no key: %+v", r)
	}
	// A key nobody knows: only its start is kept, to tell which one was used.
	e.call("GET", "/api/v1/reservations?externalRef=x", nil, map[string]string{"Authorization": "Bearer mdm_oldKeyFromBefore-0123456789"})
	if r := lastRecord(t, e); r.Status != 401 || r.KeyHint != "mdm_oldKey" {
		t.Fatalf("unknown key: %+v", r)
	}
	// Refused although the key is right: the client is named, and so is the address.
	elsewhere := []string{"203.0.113.0/24"}
	e.repo.UpdateClient(t.Context(), e.client, postgres.ClientUpdate{IPAllowlist: &elsewhere})
	e.call("GET", "/api/v1/reservations?externalRef=x", nil, map[string]string{"CF-Connecting-IP": "198.51.100.9"})
	if r := lastRecord(t, e); r.Status != 403 || r.ErrorCode != "FORBIDDEN" || r.Client != "派發系統" || r.IP != "198.51.100.9" || r.KeyHint != "" || r.ErrorMsg == "" {
		t.Fatalf("ip refused: %+v", r)
	}
	none := []string{}
	off := false
	e.repo.UpdateClient(t.Context(), e.client, postgres.ClientUpdate{IPAllowlist: &none, Enabled: &off})
	e.call("GET", "/api/v1/reservations?externalRef=x", nil, nil)
	if r := lastRecord(t, e); r.Status != 403 || r.Client != "派發系統" {
		t.Fatalf("disabled: %+v", r)
	}
	on := true
	e.repo.UpdateClient(t.Context(), e.client, postgres.ClientUpdate{Enabled: &on})

	// A booking: what went in and what came out.
	body := booking("protrack:P-LOG", "09:16", "17:16")
	hdr := map[string]string{"Idempotency-Key": "log-key-1", "CF-Connecting-IP": "203.0.113.7"}
	created := e.call("POST", "/api/v1/reservations", body, hdr)
	if created.status != 201 {
		t.Fatalf("create: %d %s", created.status, created.raw)
	}
	r := lastRecord(t, e)
	if r.Status != 201 || r.Client != "派發系統" || r.IP != "203.0.113.7" || r.IdemKey != "log-key-1" || r.ErrorCode != "" || r.Replayed {
		t.Fatalf("create record: %+v", r)
	}
	var sent map[string]interface{}
	if err := json.Unmarshal([]byte(r.RequestBody), &sent); err != nil || sent["externalRef"] != "protrack:P-LOG" || sent["startAt"] != at(day, "09:16") {
		t.Fatalf("what came in: %q", r.RequestBody)
	}
	if !strings.Contains(r.ResponseBody, `"reservationId":"`+created.str("reservationId")+`"`) || r.ReqTruncated || r.RespTruncated {
		t.Fatalf("what went out: %q", r.ResponseBody)
	}
	// The same request again is answered from the stored result.
	e.call("POST", "/api/v1/reservations", body, hdr)
	if r := lastRecord(t, e); r.Status != 201 || !r.Replayed {
		t.Fatalf("replay: %+v", r)
	}
	// A refusal with the API's error code and message.
	e.call("POST", "/api/v1/reservations", booking("protrack:P-LOG-2", "10:00", "11:00"), map[string]string{"Idempotency-Key": "log-key-2"})
	if r := lastRecord(t, e); r.Status != 409 || r.ErrorCode != "VEHICLE_UNAVAILABLE" || r.ErrorMsg != "該時段已被預約" ||
		!strings.Contains(r.ResponseBody, "VEHICLE_UNAVAILABLE") {
		t.Fatalf("conflict: %+v", r)
	}
	e.call("POST", "/api/v1/reservations", map[string]string{"externalRef": "x"}, map[string]string{"Idempotency-Key": "log-key-3"})
	if r := lastRecord(t, e); r.Status != 400 || r.ErrorCode != "VALIDATION_FAILED" {
		t.Fatalf("validation: %+v", r)
	}
	// A body past the cap is kept up to the cap and flagged.
	huge := map[string]string{"externalRef": "protrack:big", "note": strings.Repeat("字", 9000)}
	e.call("POST", "/api/v1/reservations", huge, map[string]string{"Idempotency-Key": "log-key-4"})
	if r := lastRecord(t, e); !r.ReqTruncated || len(r.RequestBody) != maxLoggedBody {
		t.Fatalf("large body: truncated=%v len=%d", r.ReqTruncated, len(r.RequestBody))
	}

	// The health probe is not logged.
	before := len(e.reqLog.Recent(100))
	resp, _ := http.Get(e.srv.URL + "/healthz")
	resp.Body.Close()
	if len(e.reqLog.Recent(100)) != before {
		t.Fatal("/healthz must not be logged")
	}

	// Nothing secret anywhere: not the key in any record, not the bodies in the server log.
	all, _ := json.Marshal(func() []RequestRecord {
		var out []RequestRecord
		for _, s := range e.reqLog.Recent(100) {
			f, _ := e.reqLog.Get(s.ID)
			out = append(out, f)
		}
		return out
	}())
	if strings.Contains(string(all), e.key) {
		t.Fatal("the API key must never be recorded")
	}
	logged := out.String()
	for _, secret := range []string{e.key, "wang.xm@mail.isha.org.tw", "派工單", "高雄市前鎮區○○路○號", "protrack:P-LOG"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("server log leaks %q:\n%s", secret, logged)
		}
	}
	for _, want := range []string{"POST /api/v1/reservations 201", "POST /api/v1/reservations 409 VEHICLE_UNAVAILABLE", "client=派發系統",
		"ip=203.0.113.7", "GET /api/v1/reservations 401 UNAUTHORIZED", "key=mdm_oldKey…", "replayed"} {
		if !strings.Contains(logged, want) {
			t.Errorf("server log lacks %q:\n%s", want, logged)
		}
	}
}

func TestRequestLog_SettingsEndpoints(t *testing.T) {
	pool := pgtest.Pool(t)
	const secret = "test-secret"
	rl := NewRequestLog(10)
	rl.Add(RequestRecord{Method: "POST", Path: "/api/v1/reservations", Status: 409, ErrorCode: "VEHICLE_UNAVAILABLE", RequestBody: `{"a":1}`, ResponseBody: `{"error":{}}`})
	rl.Add(RequestRecord{Method: "GET", Path: "/api/v1/availability", Status: 200})

	mux := http.NewServeMux()
	NewIntegrationSettingsController(postgres.NewIntegrationRepo(pool), postgres.NewAuditRepo(pool),
		middleware.NewAuthHelper(secret, nil), ":8081", WebhookStatus{}).WithRequestLog(rl).RegisterRoutes(mux)
	web := httptest.NewServer(mux)
	defer web.Close()
	admin, _, _, _ := middleware.GenerateTokens(secret, "11111111-1111-1111-1111-111111111111", "admin", "admin", "sys_admin")
	plain, _, _, _ := middleware.GenerateTokens(secret, "22222222-2222-2222-2222-222222222222", "bob", "viewer", "user")
	get := func(token, path string) (int, map[string]interface{}) {
		req, _ := http.NewRequest("GET", web.URL+path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	for _, p := range []string{"/api/settings/integration/requests", "/api/settings/integration/requests/1"} {
		if st, _ := get(plain, p); st != 403 {
			t.Errorf("%s as a plain user: %d", p, st)
		}
		if st, _ := get("", p); st != 403 {
			t.Errorf("%s without login: %d", p, st)
		}
	}
	st, list := get(admin, "/api/settings/integration/requests")
	rows, _ := list["requests"].([]interface{})
	if st != 200 || len(rows) != 2 || rows[0].(map[string]interface{})["path"] != "/api/v1/availability" {
		t.Fatalf("list: %d %v", st, list)
	}
	if _, has := rows[1].(map[string]interface{})["request_body"]; has {
		t.Fatal("the list must not carry bodies")
	}
	if st, one := get(admin, "/api/settings/integration/requests/1"); st != 200 || one["request_body"] != `{"a":1}` || one["response_body"] != `{"error":{}}` || one["error_code"] != "VEHICLE_UNAVAILABLE" {
		t.Fatalf("detail: %d %v", st, one)
	}
	if st, _ := get(admin, "/api/settings/integration/requests/99"); st != 404 {
		t.Errorf("unknown id: %d", st)
	}
	if st, _ := get(admin, "/api/settings/integration/requests/abc"); st != 404 {
		t.Errorf("bad id: %d", st)
	}
	if _, l := get(admin, "/api/settings/integration/requests?limit=1"); len(l["requests"].([]interface{})) != 1 {
		t.Errorf("limit")
	}
}
