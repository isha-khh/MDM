package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/mdm-server/internal/adapter/postgres"
	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/port"
	"github.com/anthropics/mdm-server/internal/service"
)

// These tests cover the webhook to the dispatch system: which actions in the
// web UI queue an event, what the event says, and how it is delivered.

const hookSecret = "test-webhook-secret"

type hookReq struct {
	body      []byte
	timestamp string
	signature string
}

type receiver struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []hookReq
	code int
}

func newReceiver(t *testing.T) *receiver {
	r := &receiver{code: 200}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.reqs = append(r.reqs, hookReq{b, req.Header.Get("X-Timestamp"), req.Header.Get("X-Signature")})
		code := r.code
		r.mu.Unlock()
		w.WriteHeader(code)
		w.Write([]byte(`{"result":"applied"}`))
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *receiver) setCode(c int) { r.mu.Lock(); r.code = c; r.mu.Unlock() }
func (r *receiver) taken() []hookReq {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.reqs
	r.reqs = nil
	return out
}

type webEnv struct {
	*apiEnv
	web    *httptest.Server
	hook   *receiver
	outbox *postgres.WebhookOutbox
	disp   *service.WebhookDispatcher
	token  string
	user   string // borrower id
}

type fakeSender struct{}

func (fakeSender) Send(context.Context, string, string, string) error { return nil }

func newWebEnv(t *testing.T) *webEnv {
	t.Helper()
	e := &webEnv{apiEnv: newAPIEnv(t), hook: newReceiver(t)}
	pool := e.pool
	e.outbox = postgres.NewWebhookOutbox(pool)
	events := service.NewReservationEvents(postgres.NewReservationRepo(pool), e.outbox)
	e.disp = service.NewWebhookDispatcher(e.outbox, e.hook.srv.URL, hookSecret)

	userRepo := postgres.NewUserRepo(pool)
	auth := middleware.NewAuthHelper("test-secret", nil)
	var notifRepo port.NotificationRepository = postgres.NewNotificationRepo(pool)
	notify := service.NewNotifyService(fakeSender{}, notifRepo, userRepo)
	rc := NewRentalController(postgres.NewRentalRepo(pool), postgres.NewAssetRepo(pool), userRepo, notify, auth,
		postgres.NewCategoryRepo(pool), postgres.NewCategoryRentalRuleRepo(pool), postgres.NewRentalDailyReportRepo(pool),
		postgres.NewChecklistTemplateRepo(pool), postgres.NewRentalExtensionRepo(pool)).WithEvents(events)
	mux := http.NewServeMux()
	rc.RegisterRoutes(mux)
	e.web = httptest.NewServer(mux)
	t.Cleanup(e.web.Close)

	// The approver recorded on a rental must be a real user.
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, username, password_hash, role) VALUES ('11111111-1111-1111-1111-111111111111','admin.chen','x','admin')`); err != nil {
		t.Fatal(err)
	}
	e.token, _, _, _ = middleware.GenerateTokens("test-secret", "11111111-1111-1111-1111-111111111111", "admin.chen", "admin", "sys_admin")
	e.user = e.apApiUser("borrower.b", "borrower.b@mail.isha.org.tw")
	return e
}

func (e *webEnv) apApiUser(name, email string) string { return e.apiEnv.user(name, email, "", true) }

func (e *webEnv) webCall(method, path string, body interface{}) (int, map[string]interface{}) {
	e.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.web.URL+path, rdr)
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// rentCar books the car on the web the way the rentals page does.
func (e *webEnv) rentCar(asset string) (id string, number int) {
	e.t.Helper()
	today := time.Now().In(domain.TaipeiZone).Format("2006-01-02")
	st, out := e.webCall("POST", "/api/rentals", map[string]interface{}{
		"asset_ids": []string{asset}, "borrower_id": e.user, "purpose": "公務", "borrow_date": today, "expected_return": today,
	})
	if st != 200 {
		e.t.Fatalf("web create: %d %v", st, out)
	}
	ids := out["ids"].([]interface{})
	return ids[0].(string), int(out["rental_number"].(float64))
}

// events returns the queued events, oldest first, as decoded JSON.
func (e *webEnv) events() []map[string]interface{} {
	e.t.Helper()
	rows, err := e.pool.Query(context.Background(), `SELECT body FROM integration_webhook_outbox ORDER BY id`)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var b []byte
		rows.Scan(&b)
		var m map[string]interface{}
		json.Unmarshal(b, &m)
		out = append(out, m)
	}
	return out
}

func types(evs []map[string]interface{}) []string {
	var out []string
	for _, ev := range evs {
		out = append(out, ev["eventType"].(string))
	}
	return out
}

func TestWebhook_CreatedInTheWebUI(t *testing.T) {
	e := newWebEnv(t)
	_, number := e.rentCar(e.asset)

	evs := e.events()
	if len(evs) != 1 || evs[0]["eventType"] != "reservation.created" {
		t.Fatalf("events: %v", types(evs))
	}
	ev := evs[0]
	rv := ev["reservation"].(map[string]interface{})
	if ev["reservationId"] != "R-"+strconv.Itoa(number) || ev["externalRef"] != nil || ev["previous"] != nil ||
		ev["changedBy"] != "admin.chen" || rv["status"] != "RESERVED" || rv["resourceId"] != "RENTAL_CAR_01" ||
		rv["borrowerEmail"] != "borrower.b@mail.isha.org.tw" {
		t.Fatalf("event: %v", ev)
	}
	// A whole-day web booking is reported as the whole day.
	today := time.Now().In(domain.TaipeiZone)
	if rv["startAt"] != time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, domain.TaipeiZone).Format(time.RFC3339) {
		t.Errorf("startAt %v", rv["startAt"])
	}
	if id, _ := ev["eventId"].(string); len(id) != len("mdm-evt-20261029-000123") || id[:8] != "mdm-evt-" {
		t.Errorf("eventId %q", id)
	}
}

func TestWebhook_OtherAssetsAreNotReported(t *testing.T) {
	e := newWebEnv(t)
	var laptop string
	e.pool.QueryRow(context.Background(), `INSERT INTO assets (asset_number, name, is_rentable) VALUES ('NB-01','筆電',TRUE) RETURNING id`).Scan(&laptop)
	id, _ := e.rentCar(laptop)
	e.webCall("POST", "/api/rentals/"+id+"/approve", nil)
	e.webCall("POST", "/api/rentals/"+id+"/activate", nil)
	e.webCall("DELETE", "/api/rentals/"+id, nil)
	if n := e.count(`SELECT count(*) FROM integration_webhook_outbox`); n != 0 {
		t.Fatalf("a laptop rental queued %d events", n)
	}
}

func TestWebhook_LifecycleInTheWebUI(t *testing.T) {
	e := newWebEnv(t)
	id, _ := e.rentCar(e.asset)

	if st, _ := e.webCall("POST", "/api/rentals/"+id+"/approve", nil); st != 200 {
		t.Fatalf("approve: %d", st)
	}
	if got := types(e.events()); len(got) != 1 {
		t.Fatalf("approving keeps it RESERVED and says nothing: %v", got)
	}
	if st, out := e.webCall("POST", "/api/rentals/"+id+"/activate", nil); st != 200 {
		t.Fatalf("activate: %d %v", st, out)
	}
	evs := e.events()
	if got := types(evs); len(got) != 2 || got[1] != "reservation.status_changed" {
		t.Fatalf("handing over: %v", got)
	}
	if evs[1]["reservation"].(map[string]interface{})["status"] != "IN_USE" || evs[1]["previous"].(map[string]interface{})["status"] != "RESERVED" {
		t.Fatalf("status change: %v", evs[1])
	}
	// An action that fails changes nothing and queues nothing.
	if st, _ := e.webCall("POST", "/api/rentals/"+id+"/approve", nil); st != 400 {
		t.Fatalf("approve twice: %d", st)
	}
	if got := types(e.events()); len(got) != 2 {
		t.Fatalf("failed action queued an event: %v", got)
	}
}

func TestWebhook_RejectAndDeleteAreCancellations(t *testing.T) {
	e := newWebEnv(t)
	id, _ := e.rentCar(e.asset)
	if st, out := e.webCall("POST", "/api/rentals/"+id+"/reject", nil); st != 200 {
		t.Fatalf("reject: %d %v", st, out)
	}
	evs := e.events()
	if got := types(evs); len(got) != 2 || got[1] != "reservation.cancelled" || evs[1]["reservation"].(map[string]interface{})["status"] != "CANCELLED" {
		var st string
		e.pool.QueryRow(context.Background(), `SELECT status FROM rentals WHERE id=$1`, id).Scan(&st)
		t.Fatalf("reject: %v (rental status %s)", got, st)
	}
	// Deleting a reservation that is gone from the list is still a cancellation for the other side.
	id2, _ := e.rentCar(e.asset)
	e.webCall("DELETE", "/api/rentals/"+id2, nil)
	evs = e.events()
	if got := types(evs); len(got) != 4 || got[3] != "reservation.cancelled" || evs[3]["reservation"].(map[string]interface{})["status"] != "CANCELLED" {
		t.Fatalf("delete: %v", got)
	}
	// Deleting one that was already cancelled adds nothing.
	e.webCall("DELETE", "/api/rentals/"+id, nil)
	if got := types(e.events()); len(got) != 4 {
		t.Fatalf("deleting a cancelled rental: %v", got)
	}
}

func TestWebhook_ChangesToAReservationMadeThroughTheAPI(t *testing.T) {
	e := newWebEnv(t)
	e.apiEnv.user("wang.xm", "wang.xm@mail.isha.org.tw", "sub-wang", true)
	r := e.write("POST", "/api/v1/reservations", booking("protrack:P-88", "09:00", "17:00"))
	if r.status != 201 {
		t.Fatalf("create via API: %d %s", r.status, r.raw)
	}
	// The API's own changes are not echoed back to the caller.
	e.write("PUT", "/api/v1/reservations/"+r.str("reservationId"), map[string]string{"note": "x"})
	if n := e.count(`SELECT count(*) FROM integration_webhook_outbox`); n != 0 {
		t.Fatalf("API-made changes queued %d events", n)
	}
	// What staff do to it in the web UI is.
	var id string
	e.pool.QueryRow(context.Background(), `SELECT id FROM rentals WHERE external_ref='protrack:P-88'`).Scan(&id)
	if st, _ := e.webCall("POST", "/api/rentals/"+id+"/activate", nil); st != 200 {
		t.Fatalf("activate: %d", st)
	}
	evs := e.events()
	if len(evs) != 1 || evs[0]["eventType"] != "reservation.status_changed" || evs[0]["externalRef"] != "protrack:P-88" ||
		evs[0]["reservation"].(map[string]interface{})["borrowerEmployeeId"] != "wang.xm" {
		t.Fatalf("events: %v", evs)
	}
	if st, _ := e.webCall("DELETE", "/api/rentals/"+id, nil); st != 200 {
		t.Fatalf("delete: %d", st)
	}
	if got := types(e.events()); len(got) != 2 || got[1] != "reservation.cancelled" {
		t.Fatalf("delete: %v", got)
	}
}

func TestWebhook_ExtensionMovesTheEnd(t *testing.T) {
	e := newWebEnv(t)
	id, number := e.rentCar(e.asset)
	e.webCall("POST", "/api/rentals/"+id+"/approve", nil)
	e.webCall("POST", "/api/rentals/"+id+"/activate", nil)
	tomorrow := time.Now().In(domain.TaipeiZone).AddDate(0, 0, 1).Format("2006-01-02")
	if st, out := e.webCall("POST", "/api/rentals/"+id+"/extend", map[string]string{"new_expected_return": tomorrow, "reason": "路程較遠"}); st != 200 {
		t.Fatalf("extend request: %d %v", st, out)
	}
	var extID string
	e.pool.QueryRow(context.Background(), `SELECT id FROM rental_extensions WHERE rental_number=$1`, number).Scan(&extID)
	before := len(e.events())
	if st, out := e.webCall("POST", "/api/rental-extensions/"+extID+"/approve", map[string]string{"note": "同意"}); st != 200 {
		t.Fatalf("approve extension: %d %v", st, out)
	}
	evs := e.events()
	if len(evs) != before+1 {
		t.Fatalf("events: %v", types(evs))
	}
	ev := evs[len(evs)-1]
	if ev["eventType"] != "reservation.changed" || ev["note"] != "同意" || ev["changedBy"] != "admin.chen" ||
		ev["reservation"].(map[string]interface{})["endAt"] == ev["previous"].(map[string]interface{})["endAt"] {
		t.Fatalf("extension event: %v", ev)
	}
}

func TestWebhook_Delivery(t *testing.T) {
	e := newWebEnv(t)
	ctx := context.Background()
	e.rentCar(e.asset)

	if n := e.disp.DeliverDue(ctx); n != 1 {
		t.Fatalf("delivered %d", n)
	}
	got := e.hook.taken()
	if len(got) != 1 {
		t.Fatalf("receiver got %d", len(got))
	}
	ts, _ := strconv.ParseInt(got[0].timestamp, 10, 64)
	if d := time.Since(time.Unix(ts, 0)); d < -time.Minute || d > time.Minute {
		t.Errorf("timestamp %s is not now", got[0].timestamp)
	}
	if got[0].signature != service.SignWebhook(hookSecret, got[0].timestamp, got[0].body) {
		t.Errorf("signature does not verify")
	}
	if service.SignWebhook("another-secret", got[0].timestamp, got[0].body) == got[0].signature {
		t.Errorf("signature should depend on the secret")
	}
	if n := e.count(`SELECT count(*) FROM integration_webhook_outbox WHERE status='sent' AND sent_at IS NOT NULL`); n != 1 {
		t.Fatalf("event not marked sent")
	}
	if n := e.disp.DeliverDue(ctx); n != 0 {
		t.Fatalf("a sent event is not sent again (%d)", n)
	}
}

func TestWebhook_RetriesWithTheSameEvent(t *testing.T) {
	e := newWebEnv(t)
	ctx := context.Background()
	e.rentCar(e.asset)
	makeDue := func() {
		e.pool.Exec(ctx, `UPDATE integration_webhook_outbox SET next_attempt_at = now() WHERE status='pending'`)
	}

	e.hook.setCode(503)
	e.disp.DeliverDue(ctx)
	first := e.hook.taken()
	if len(first) != 1 {
		t.Fatalf("first try: %d", len(first))
	}
	var attempts int
	var inFuture bool
	e.pool.QueryRow(ctx, `SELECT attempts, next_attempt_at > now() + interval '30 seconds' FROM integration_webhook_outbox`).Scan(&attempts, &inFuture)
	if attempts != 1 || !inFuture {
		t.Fatalf("after a failure: attempts=%d, next try in the future=%v", attempts, inFuture)
	}
	if n := e.disp.DeliverDue(ctx); n != 0 {
		t.Fatalf("not due yet, but %d were sent", n)
	}

	makeDue()
	e.hook.setCode(200)
	e.disp.DeliverDue(ctx)
	second := e.hook.taken()
	if len(second) != 1 || !bytes.Equal(second[0].body, first[0].body) {
		t.Fatalf("the retry must carry the same body (and so the same eventId)")
	}
	if n := e.count(`SELECT count(*) FROM integration_webhook_outbox WHERE status='sent' AND attempts=2`); n != 1 {
		t.Fatalf("retry should have succeeded on the second try")
	}
}

func TestWebhook_GivesUpAfterFiveRetries(t *testing.T) {
	e := newWebEnv(t)
	ctx := context.Background()
	e.rentCar(e.asset)
	e.hook.setCode(500)
	for i := 0; i < 6; i++ {
		e.pool.Exec(ctx, `UPDATE integration_webhook_outbox SET next_attempt_at = now() WHERE status='pending'`)
		e.disp.DeliverDue(ctx)
	}
	if n := len(e.hook.taken()); n != 6 {
		t.Fatalf("tries = %d, want the first and five retries", n)
	}
	if n := e.count(`SELECT count(*) FROM integration_webhook_outbox WHERE status='failed' AND attempts=6 AND last_error LIKE 'HTTP 500%'`); n != 1 {
		t.Fatalf("event should be given up with its error kept")
	}
	e.pool.Exec(ctx, `UPDATE integration_webhook_outbox SET next_attempt_at = now()`)
	if n := e.disp.DeliverDue(ctx); n != 0 {
		t.Fatalf("a given-up event is not sent again (%d)", n)
	}
	// An administrator can put them back.
	if n, err := e.outbox.RetryFailed(ctx); err != nil || n != 1 {
		t.Fatalf("retry failed: %d %v", n, err)
	}
	e.hook.setCode(200)
	if n := e.disp.DeliverDue(ctx); n != 1 {
		t.Fatalf("requeued event: %d", n)
	}
}

func TestWebhook_BadRequestIsNotRetried(t *testing.T) {
	e := newWebEnv(t)
	ctx := context.Background()
	e.rentCar(e.asset)
	e.hook.setCode(400)
	e.disp.DeliverDue(ctx)
	if n := e.count(`SELECT count(*) FROM integration_webhook_outbox WHERE status='failed' AND attempts=1`); n != 1 {
		t.Fatalf("a 400 means the event itself is wrong; do not retry it")
	}
	// 401 (wrong secret / clock) can be fixed on the other side, so it is retried.
	e.rentCar(e.asset2())
	e.hook.setCode(401)
	e.disp.DeliverDue(ctx)
	if n := e.count(`SELECT count(*) FROM integration_webhook_outbox WHERE status='pending' AND attempts=1`); n != 1 {
		t.Fatalf("a 401 should be retried")
	}
}

// asset2 is a second car (its own resource), so a second rental can be made.
func (e *webEnv) asset2() string {
	var id string
	e.pool.QueryRow(context.Background(), `INSERT INTO assets (asset_number, name, is_rentable) VALUES ('CAR-02','公務車 2',TRUE) RETURNING id`).Scan(&id)
	e.repo.UpsertResource(context.Background(), &domain.IntegrationResource{ResourceID: "RENTAL_CAR_02", ServiceType: "RENTAL_CAR", AssetID: id, Enabled: true})
	return id
}

func TestWebhook_EventsOfOneReservationGoInOrder(t *testing.T) {
	e := newWebEnv(t)
	ctx := context.Background()
	id, _ := e.rentCar(e.asset)
	e.webCall("POST", "/api/rentals/"+id+"/approve", nil)
	e.webCall("POST", "/api/rentals/"+id+"/activate", nil) // second event: status_changed
	if n := e.count(`SELECT count(*) FROM integration_webhook_outbox`); n != 2 {
		t.Fatalf("setup: %d events", n)
	}
	// The first event cannot be delivered, so the second must wait behind it.
	e.hook.setCode(500)
	for i := 0; i < 2; i++ {
		e.disp.DeliverDue(ctx)
	}
	got := e.hook.taken()
	if len(got) != 1 {
		t.Fatalf("receiver saw %d calls; the later event must not overtake the earlier one", len(got))
	}
	var first map[string]interface{}
	json.Unmarshal(got[0].body, &first)
	if first["eventType"] != "reservation.created" {
		t.Fatalf("first delivered: %v", first["eventType"])
	}
	// Once it goes through, the next follows.
	e.hook.setCode(200)
	e.pool.Exec(ctx, `UPDATE integration_webhook_outbox SET next_attempt_at = now() WHERE status='pending'`)
	e.disp.DeliverDue(ctx)
	e.disp.DeliverDue(ctx)
	var order []string
	for _, h := range e.hook.taken() {
		var m map[string]interface{}
		json.Unmarshal(h.body, &m)
		order = append(order, m["eventType"].(string))
	}
	if fmt.Sprint(order) != "[reservation.created reservation.status_changed]" {
		t.Fatalf("order: %v", order)
	}
}
