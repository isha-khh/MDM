package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/mdm-server/internal/adapter/google"
	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/service/tripcar"
)

// fakeNotices keeps the claim rules of the SQL repo: a notice that is sent or
// in flight can't be claimed again; a failed one can.
type fakeNotices struct {
	mu       sync.Mutex
	rows     map[string]*domain.TripCarNotice
	nextID   int64
	claimErr error
	listErr  error
}

func newFakeNotices() *fakeNotices { return &fakeNotices{rows: map[string]*domain.TripCarNotice{}} }

func (f *fakeNotices) Claim(_ context.Context, n *domain.TripCarNotice, _ string) (int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claimErr != nil {
		return 0, false, f.claimErr
	}
	k := noticeKey(n.CalendarID, n.EventID, n.EventStart, n.RecipientEmail)
	if old, ok := f.rows[k]; ok {
		if old.Status != domain.NoticeFailed {
			return 0, false, nil
		}
		old.Status, old.Error = domain.NoticePending, ""
		return old.ID, true, nil
	}
	f.nextID++
	c := *n
	c.ID, c.Status = f.nextID, domain.NoticePending
	f.rows[k] = &c
	return c.ID, true, nil
}

func (f *fakeNotices) find(id int64) *domain.TripCarNotice {
	for _, r := range f.rows {
		if r.ID == id {
			return r
		}
	}
	return nil
}

func (f *fakeNotices) MarkSent(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.find(id)
	now := time.Date(2026, 10, 20, 9, 0, 1, 0, time.UTC)
	r.Status, r.SentAt = domain.NoticeSent, &now
	return nil
}

func (f *fakeNotices) MarkFailed(_ context.Context, id int64, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.find(id)
	r.Status, r.Error = domain.NoticeFailed, reason
	return nil
}

func (f *fakeNotices) ListFromDate(context.Context, string) ([]domain.TripCarNotice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []domain.TripCarNotice
	for _, r := range f.rows {
		out = append(out, *r)
	}
	return out, nil
}

type sentMail struct{ to, subject, body string }

type fakeMailer struct {
	disabled bool
	failFor  map[string]error
	sent     []sentMail
}

func (m *fakeMailer) Enabled() bool { return !m.disabled }
func (m *fakeMailer) Send(_ context.Context, to, subject, body string) error {
	if err := m.failFor[to]; err != nil {
		return err
	}
	m.sent = append(m.sent, sentMail{to, subject, body})
	return nil
}

func (m *fakeMailer) recipients() []string {
	var out []string
	for _, s := range m.sent {
		out = append(out, s.to)
	}
	return out
}

const sendPath = basePath + "/notices/send"

// mailboxOf are the sample directory people's mailboxes; anyone else gets a
// made-up one the directory doesn't know.
var mailboxOf = map[string]string{
	"夏宗瑜": "zoey.xia@mail.isha.org.tw", "羅羿昇": "yishengluo1006@mail.isha.org.tw", "李念芷": "s01023@mail.isha.org.tw",
}

// tripDesc is a description in the mandatory format, every field filled, with
// one mailbox per name in the same order.
func tripDesc(location string, names ...string) string {
	var boxes []string
	for _, n := range names {
		plain := strings.SplitN(n, "(", 2)[0]
		box := mailboxOf[plain]
		if box == "" {
			box = "unknown-" + plain + "@mail.isha.org.tw"
		}
		boxes = append(boxes, box)
	}
	return "地點：" + location + "\n時間：09:00-12:00\n交通：高鐵＋租車\n協會人員：" + strings.Join(names, "、") + "\n信箱：" + strings.Join(boxes, "、")
}

// noticeEnv: today is 2026-10-20 (Taipei); the directory has the sample people.
// "e-two" starts Oct 27 (7 days: window opens today) with four attendees.
func noticeEnv(t *testing.T) (*env, *fakeNotices, *fakeMailer) {
	t.Helper()
	e := previewEnv(t)
	e.dir.users = directoryUsers()
	e.g.events["ida@group"] = append(e.g.events["ida@group"], google.Event{
		ID: "e-two", Summary: "76工危品進廠(臺南分局)", Start: "2026-10-27", AllDay: true,
		Description: tripDesc("臺南分局", "夏宗瑜(管考)", "羅羿昇", "李念芷", "路人甲"),
	})
	n, m := newFakeNotices(), &fakeMailer{failFor: map[string]error{}}
	e.c.WithNotices(n, m)
	return e, n, m
}

type sendResp struct {
	Results    []noticeResult    `json:"results"`
	Counts     map[string]int    `json:"counts"`
	Incomplete []incompleteEvent `json:"incomplete"`
}

func postSend(t *testing.T, e *env, body string) (int, sendResp, string) {
	t.Helper()
	rec := do(e.mux, "POST", sendPath, tokenFor(t, "admin", "sys_admin"), "application/json", []byte(body))
	var out sendResp
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out, rec.Body.String()
}

func TestSend_MailsOnlyLicensedDueAttendeesAndOnlyOnce(t *testing.T) {
	e, notices, mailer := noticeEnv(t)

	code, out, raw := postSend(t, e, "")
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	// Due, target and in the mandatory format: only e-two. Of its attendees,
	// 羅羿昇 and 夏宗瑜 are licensed; 李念芷 has no license data and 路人甲 isn't in
	// the directory. e-visit and e-khh are due targets too, but their
	// descriptions lack required fields, so they are reported, not mailed.
	got := mailer.recipients()
	want := map[string]int{"yishengluo1006@mail.isha.org.tw": 1, "zoey.xia@mail.isha.org.tw": 1}
	if len(got) != 2 {
		t.Fatalf("recipients: %v", got)
	}
	for _, r := range got {
		want[r]--
	}
	for r, n := range want {
		if n != 0 {
			t.Errorf("%s: off by %d (got %v)", r, n, got)
		}
	}
	if out.Counts["sent"] != 2 || out.Counts["failed"] != 0 {
		t.Errorf("counts %+v", out.Counts)
	}
	if len(notices.rows) != 2 {
		t.Errorf("log should hold exactly the 2 mails sent: %d rows", len(notices.rows))
	}
	for _, m := range mailer.sent {
		if !strings.Contains(m.subject, "出差借車提醒") || !strings.Contains(m.body, "您好") {
			t.Errorf("mail content: %+v", m)
		}
	}
	two := mailer.sent[0]
	for _, m := range mailer.sent {
		if strings.Contains(m.subject, "2026-10-27") {
			two = m
		}
	}
	if !strings.Contains(two.subject, "2026-10-27") || !strings.Contains(two.body, "09:00-12:00") || !strings.Contains(two.body, "7 天後") {
		t.Errorf("e-two mail: %+v", two)
	}

	// Pressing again mails nobody.
	before := len(mailer.sent)
	code, out, _ = postSend(t, e, "")
	if code != 200 || len(mailer.sent) != before || out.Counts["sent"] != 0 || out.Counts["already_sent"] != 2 {
		t.Errorf("second press: %d sent=%d counts=%+v", code, len(mailer.sent)-before, out.Counts)
	}

	// The preview now says who was notified.
	_, prev := previewWithDirectory(t, e)
	var seen int
	for _, ev := range prev.Events {
		for _, c := range ev.Candidates {
			if c.NoticeStatus == "sent" {
				seen++
				if c.NoticeSentAt == "" {
					t.Errorf("sent notice should carry a time: %+v", c)
				}
			}
		}
	}
	if seen != 2 {
		t.Errorf("preview shows %d as notified, want 2", seen)
	}
}

func TestSend_WindowIsRespected(t *testing.T) {
	e, _, mailer := noticeEnv(t)
	// Oct 28 is 8 days away: the window hasn't opened. A trip that started today is too late.
	e.g.events["ida@group"] = []google.Event{
		{ID: "far", Summary: "遠(臺南分局)", Start: "2026-10-28", AllDay: true, Description: tripDesc("臺南", "羅羿昇")},
		{ID: "late", Summary: "已出發(臺南分局)", Start: "2026-10-20", AllDay: true, Description: tripDesc("臺南", "羅羿昇")},
	}
	e.g.events["bip@group"] = nil
	if code, out, raw := postSend(t, e, ""); code != 200 || len(mailer.sent) != 0 || out.Counts["sent"] != 0 {
		t.Errorf("%d %s sent=%d", code, raw, len(mailer.sent))
	}
}

func TestSend_FailureIsLoggedAndRetriedWithoutResendingTheRest(t *testing.T) {
	e, notices, mailer := noticeEnv(t)
	mailer.failFor["zoey.xia@mail.isha.org.tw"] = errors.New("smtp: 550 mailbox unavailable")

	_, out, _ := postSend(t, e, "")
	if out.Counts["sent"] != 1 || out.Counts["failed"] != 1 {
		t.Fatalf("counts %+v", out.Counts)
	}
	var failed *domain.TripCarNotice
	for _, r := range notices.rows {
		if r.Status == domain.NoticeFailed {
			failed = r
		}
	}
	if failed == nil || failed.RecipientEmail != "zoey.xia@mail.isha.org.tw" || !strings.Contains(failed.Error, "550") {
		t.Fatalf("failed row: %+v", failed)
	}

	// The mailbox is fixed; pressing again mails only that person.
	delete(mailer.failFor, "zoey.xia@mail.isha.org.tw")
	mailer.sent = nil
	_, out, _ = postSend(t, e, "")
	if got := mailer.recipients(); len(got) != 1 || got[0] != "zoey.xia@mail.isha.org.tw" || out.Counts["sent"] != 1 {
		t.Errorf("retry: %v %+v", got, out.Counts)
	}
}

func TestSend_RescheduledTripIsAnnouncedAgain(t *testing.T) {
	e, _, mailer := noticeEnv(t)
	e.g.events["bip@group"] = nil
	e.g.events["ida@group"] = []google.Event{{ID: "t", Summary: "進廠(臺南分局)", Start: "2026-10-26", AllDay: true, Description: tripDesc("臺南", "羅羿昇")}}
	postSend(t, e, "")
	e.g.events["ida@group"][0].Start = "2026-10-27" // moved a day
	postSend(t, e, "")
	if len(mailer.sent) != 2 || !strings.Contains(mailer.sent[1].subject, "2026-10-27") {
		t.Errorf("a new date should be mailed again: %+v", mailer.sent)
	}
}

func TestSend_SingleEvent(t *testing.T) {
	e, _, mailer := noticeEnv(t)
	code, out, _ := postSend(t, e, `{"calendar_id":"ida@group","event_id":"e-two"}`)
	if code != 200 || out.Counts["sent"] != 2 {
		t.Fatalf("%d %+v", code, out.Counts)
	}
	for _, m := range mailer.sent {
		if !strings.Contains(m.subject, "2026-10-27") {
			t.Errorf("only e-two should have been mailed: %q", m.subject)
		}
	}
	if code, _, _ := postSend(t, e, `{"calendar_id":"ida@group","event_id":"nope"}`); code != 404 {
		t.Errorf("unknown event: %d", code)
	}
	if code, _, _ := postSend(t, e, `{"event_id":"e-two"}`); code != 400 {
		t.Errorf("event without calendar: %d", code)
	}
	if code, _, _ := postSend(t, e, `{bad`); code != 400 {
		t.Errorf("garbage body: %d", code)
	}
}

func TestSend_RefusesWhenItCannotBeSure(t *testing.T) {
	cases := map[string]func(e *env, n *fakeNotices, m *fakeMailer){
		"directory not configured": func(e *env, _ *fakeNotices, _ *fakeMailer) { e.dir.err = ErrDirectoryNotConfigured },
		"directory failing":        func(e *env, _ *fakeNotices, _ *fakeMailer) { e.dir.err = errors.New("目錄服務拒絕了 API key") },
		"no directory wired":       func(e *env, _ *fakeNotices, _ *fakeMailer) { e.c.directory = nil },
		"smtp not configured":      func(_ *env, _ *fakeNotices, m *fakeMailer) { m.disabled = true },
	}
	for name, setup := range cases {
		e, notices, mailer := noticeEnv(t)
		setup(e, notices, mailer)
		code, _, raw := postSend(t, e, "")
		if code != 409 && code != 502 {
			t.Errorf("%s: status %d %s", name, code, raw)
		}
		if len(mailer.sent) != 0 || len(notices.rows) != 0 {
			t.Errorf("%s: nothing may be sent or logged (sent=%d rows=%d)", name, len(mailer.sent), len(notices.rows))
		}
	}

	e := previewEnv(t) // sending never wired
	if code, _, _ := postSend(t, e, ""); code != 503 {
		t.Errorf("not wired: %d", code)
	}
}

func TestSend_LogFailureMeansNoMail(t *testing.T) {
	e, notices, mailer := noticeEnv(t)
	notices.claimErr = errors.New("db down")
	code, out, _ := postSend(t, e, "")
	if code != 200 || len(mailer.sent) != 0 || out.Counts["failed"] != 2 {
		t.Errorf("if the log can't be written, no mail may go out: %d sent=%d %+v", code, len(mailer.sent), out.Counts)
	}
}

func TestSend_PreviewStillWorksWhenTheLogCannotBeRead(t *testing.T) {
	e, notices, _ := noticeEnv(t)
	notices.listErr = errors.New("db down")
	rec, out := previewWithDirectory(t, e)
	if rec.Code != 200 || len(out.Events) != 6 {
		t.Errorf("%d events=%d", rec.Code, len(out.Events))
	}
}

func TestSend_RequiresSysAdmin(t *testing.T) {
	e, notices, mailer := noticeEnv(t)
	for name, tok := range map[string]string{"anonymous": "", "ordinary user": tokenFor(t, "user", "")} {
		if rec := do(e.mux, "POST", sendPath, tok, "application/json", nil); rec.Code != 401 {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if rec := do(e.mux, "GET", sendPath, tokenFor(t, "admin", "sys_admin"), "", nil); rec.Code != 405 {
		t.Errorf("GET: %d", rec.Code)
	}
	if len(mailer.sent) != 0 || len(notices.rows) != 0 {
		t.Error("an unauthorised request had an effect")
	}
}

func TestSend_OnlyMailsEventsInTheMandatoryFormat(t *testing.T) {
	e, notices, mailer := noticeEnv(t)
	e.g.events["bip@group"] = nil
	ev := func(id, desc string) google.Event {
		return google.Event{ID: id, Summary: id + "(臺南分局)", Start: "2026-10-27", AllDay: true, Description: desc}
	}
	full := tripDesc("臺南", "羅羿昇")
	e.g.events["ida@group"] = []google.Event{
		ev("ok", full),
		ev("no-time", strings.Replace(full, "時間：09:00-12:00\n", "", 1)),
		ev("no-transport", strings.Replace(full, "交通：高鐵＋租車\n", "", 1)),
		ev("no-mail", strings.Replace(full, "\n信箱：yishengluo1006@mail.isha.org.tw", "", 1)),
		ev("placeholders", "地點：臺南\n時間：—\n交通：—\n協會人員：羅羿昇\n信箱：—"),
		ev("no-names", strings.Replace(full, "協會人員：羅羿昇\n", "協會人員：—\n", 1)),
		ev("unpaired", strings.Replace(full, "協會人員：羅羿昇", "協會人員：羅羿昇、夏宗瑜", 1)), // 2 names, 1 mailbox
	}
	code, out, raw := postSend(t, e, "")
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	if len(mailer.sent) != 1 || !strings.Contains(mailer.sent[0].to, "yishengluo1006") || len(notices.rows) != 1 {
		t.Fatalf("only the complete event may be mailed: %v", mailer.recipients())
	}
	missing := map[string]string{}
	for _, inc := range out.Incomplete {
		missing[inc.EventID] = strings.Join(inc.Missing, ",")
	}
	want := map[string]string{
		"no-time": "時間", "no-transport": "交通", "no-mail": "信箱",
		"placeholders": "時間,交通,信箱", "no-names": "協會人員", "unpaired": "有人員沒有對應的信箱",
	}
	if len(missing) != len(want) {
		t.Errorf("incomplete: %v", missing)
	}
	for id, w := range want {
		if missing[id] != w {
			t.Errorf("%s: missing %q, want %q", id, missing[id], w)
		}
	}

	// The preview shows the same, per event.
	_, prev := previewWithDirectory(t, e)
	for _, pe := range prev.Events {
		if pe.ID == "ok" && len(pe.FormatMissing) != 0 {
			t.Errorf("complete event flagged: %v", pe.FormatMissing)
		}
		if pe.ID == "no-time" && strings.Join(pe.FormatMissing, ",") != "時間" {
			t.Errorf("preview missing: %v", pe.FormatMissing)
		}
	}
}

const realAgreedSampleDesc = `地點:經濟部園區管理局(811高雄市楠梓區瑞屏里加昌路600號)、中油林園廠(832高雄市林園區五福里石化二路3號)
時間:1000-1200
交通:開車
協會人員:劉名政、吳宜蓁
信箱:foylaou0326@mail.isha.org.tw、idsl5397@mail.isha.org.tw`

func TestSend_RealAgreedSampleEndToEnd(t *testing.T) {
	e, notices, mailer := noticeEnv(t)
	e.dir.users = append(directoryUsers(),
		tripcarUser("foylaou0326", "26安環處-高雄-劉名政", "foylaou0326@mail.isha.org.tw", bp(true), bp(false)))
	e.g.events["bip@group"] = nil
	e.g.events["ida@group"] = []google.Event{{
		ID: "real", Summary: "園區查核", Start: "2026-10-23T10:00:00+08:00", Description: realAgreedSampleDesc,
	}}

	// Preview: a complete target event with two paired attendees.
	_, prev := previewWithDirectory(t, e)
	ev := prev.event("real")
	if !ev.Target || len(ev.FormatMissing) != 0 || !ev.NoticeDue || ev.DaysUntil != 3 {
		t.Fatalf("event: %+v", ev)
	}
	if len(ev.Candidates) != 2 || ev.Candidates[0].Name != "劉名政" || ev.Candidates[0].Decision != "notify" ||
		ev.Candidates[1].Name != "吳宜蓁" || ev.Candidates[1].Decision != "no_license" {
		t.Fatalf("candidates: %+v", ev.Candidates)
	}

	// Send: only 劉名政 (licensed) is mailed, to the mailbox in the event.
	code, out, raw := postSend(t, e, "")
	if code != 200 || out.Counts["sent"] != 1 || len(out.Incomplete) != 0 {
		t.Fatalf("%d %s", code, raw)
	}
	m := mailer.sent[0]
	if m.to != "foylaou0326@mail.isha.org.tw" {
		t.Errorf("sent to %q", m.to)
	}
	for _, want := range []string{"劉名政 您好", "2026-10-23 10:00", "楠梓區", "林園區", "1000-1200", "開車"} {
		if !strings.Contains(m.body, want) {
			t.Errorf("mail body lacks %q", want)
		}
	}
	if strings.Contains(m.to, "idsl5397") || len(notices.rows) != 1 {
		t.Errorf("the unlicensed attendee must not be mailed or logged: rows=%d", len(notices.rows))
	}
	if strings.ContainsAny(m.subject, "\r\n") {
		t.Errorf("subject: %q", m.subject)
	}

	// Pressing again does nothing, and the preview now shows who was notified.
	if _, again, _ := postSend(t, e, ""); again.Counts["sent"] != 0 || len(mailer.sent) != 1 {
		t.Errorf("second press: %+v", again.Counts)
	}
	_, prev = previewWithDirectory(t, e)
	if c := prev.event("real").Candidates[0]; c.NoticeStatus != "sent" {
		t.Errorf("preview should show 劉名政 as notified: %+v", c)
	}
}

func tripcarUser(account, display, mail string, license, car *bool) tripcar.User {
	return tripcar.User{SamAccountName: account, DisplayName: display, Mail: mail, HasDriverLicense: license, HasOwnCar: car}
}

// ---- multi-vendor events: one "廠商：" block per stop ----

const vendorBlock = `廠商：%s
聯繫人：某某 課長 06-0000000
時間：%s
地址：%s
交通：開車
協會人員：%s
信箱：%s`

func vendor(name, time, addr, names, mails string) string {
	return fmt.Sprintf(vendorBlock, name, time, addr, names, mails)
}

const (
	tainan1 = "臺南市柳營區大農里環園西路二段19號"
	tainan2 = "臺南市新市區豐華里王甲路1號"
	zoeyBox = "zoey.xia@mail.isha.org.tw"
	testBox = "isha.khh.owner@gmail.com"
)

// multiEnv: today is Oct 23 (Taipei), so a trip on Oct 29 is 6 days away.
func multiEnv(t *testing.T, desc string) (*env, *fakeNotices, *fakeMailer) {
	t.Helper()
	e, n, m := noticeEnv(t)
	e.now = time.Date(2026, 10, 23, 1, 0, 0, 0, time.UTC)
	e.g.events["bip@group"] = nil
	e.g.events["ida@group"] = []google.Event{{ID: "mv", Summary: "[深入輔導]台灣瑞環、奇菱科技(宗瑜)", Start: "2026-10-29", AllDay: true, Description: desc}}
	return e, n, m
}

func TestMultiVendor_OnePersonOneMailListingEveryStop(t *testing.T) {
	desc := vendor("台灣瑞環", "0900-1200", tainan1, "夏宗瑜", zoeyBox+"\n"+testBox) + "\n\n" +
		vendor("奇菱科技", "1330-1700", tainan2, "夏宗瑜", zoeyBox+"\n"+testBox)
	e, notices, mailer := multiEnv(t, desc)

	_, prev := previewWithDirectory(t, e)
	ev := prev.event("mv")
	if !ev.Target || len(ev.FormatMissing) != 0 || !ev.NoticeDue || ev.DaysUntil != 6 {
		t.Fatalf("event: %+v", ev)
	}
	if len(ev.Candidates) != 2 {
		t.Fatalf("a person on two stops is one candidate, plus the test mailbox: %+v", ev.Candidates)
	}
	if c := ev.Candidates[0]; c.Email != zoeyBox || c.Decision != "notify" || fmt.Sprint(c.Stops) != "[0 1]" {
		t.Errorf("zoey: %+v", c)
	}
	if c := ev.Candidates[1]; c.Email != testBox || c.Decision != "unmatched" {
		t.Errorf("the test mailbox isn't in the directory: %+v", c)
	}

	code, out, raw := postSend(t, e, "")
	if code != 200 || out.Counts["sent"] != 1 || len(mailer.sent) != 1 || len(notices.rows) != 1 {
		t.Fatalf("%d %s sent=%d", code, raw, len(mailer.sent))
	}
	m := mailer.sent[0]
	if m.to != zoeyBox {
		t.Errorf("sent to %q", m.to)
	}
	for _, want := range []string{"台灣瑞環", "奇菱科技", "柳營區大農里", "新市區豐華里", "0900-1200", "1330-1700", "2026-10-29"} {
		if !strings.Contains(m.body, want) {
			t.Errorf("mail lacks %q", want)
		}
	}
	if strings.Count(m.body, "<strong>地點</strong>") != 2 || strings.Contains(m.subject, "柳營區") {
		t.Errorf("one table per stop, subject names the trip: %q", m.subject)
	}
	if strings.Contains(strings.Join(mailer.recipients(), ","), "gmail") {
		t.Error("the mailbox the directory doesn't know must not be mailed")
	}

	// again: nothing
	if _, again, _ := postSend(t, e, ""); again.Counts["sent"] != 0 || len(mailer.sent) != 1 {
		t.Errorf("second press: %+v", again.Counts)
	}
}

func TestMultiVendor_EachPersonGetsOnlyTheirOwnStops(t *testing.T) {
	desc := vendor("台灣瑞環", "0900-1200", tainan1, "夏宗瑜", zoeyBox) + "\n\n" +
		vendor("奇菱科技", "1330-1700", tainan2, "羅羿昇", "yishengluo1006@mail.isha.org.tw")
	e, _, mailer := multiEnv(t, desc)
	postSend(t, e, "")
	if len(mailer.sent) != 2 {
		t.Fatalf("two people, two mails: %v", mailer.recipients())
	}
	for _, m := range mailer.sent {
		switch m.to {
		case zoeyBox:
			// (the event title names both vendors, so tell the stops apart by address)
			if !strings.Contains(m.body, "柳營區") || strings.Contains(m.body, "新市區") {
				t.Errorf("zoey should only see her stop")
			}
		default:
			if !strings.Contains(m.body, "新市區") || strings.Contains(m.body, "柳營區") {
				t.Errorf("羅羿昇 should only see his stop")
			}
		}
	}
}

func TestMultiVendor_OnlyTargetStopsAreInTheMail(t *testing.T) {
	desc := vendor("台中廠", "0900-1000", "台中市西屯區某路1號", "夏宗瑜", zoeyBox) + "\n\n" +
		vendor("奇菱科技", "1330-1700", tainan2, "夏宗瑜", zoeyBox)
	e, _, mailer := multiEnv(t, desc)
	postSend(t, e, "")
	if len(mailer.sent) != 1 || !strings.Contains(mailer.sent[0].body, "新市區") || strings.Contains(mailer.sent[0].body, "台中廠") {
		t.Errorf("the Taichung stop needs no rental car: %+v", mailer.sent)
	}
}

func TestMultiVendor_NoTargetStopMeansNoNotice(t *testing.T) {
	desc := vendor("台中廠", "0900-1000", "台中市西屯區某路1號", "夏宗瑜", zoeyBox)
	e, _, mailer := multiEnv(t, desc)
	_, prev := previewWithDirectory(t, e)
	if ev := prev.event("mv"); ev.Target || len(ev.Candidates) != 0 {
		t.Errorf("%+v", ev)
	}
	postSend(t, e, "")
	if len(mailer.sent) != 0 {
		t.Error("nothing to send")
	}
}

func TestMultiVendor_OneIncompleteBlockHoldsBackTheWholeEvent(t *testing.T) {
	second := strings.Replace(vendor("奇菱科技", "1330-1700", tainan2, "夏宗瑜", zoeyBox), "交通：開車\n", "", 1)
	desc := vendor("台灣瑞環", "0900-1200", tainan1, "夏宗瑜", zoeyBox) + "\n\n" + second
	e, notices, mailer := multiEnv(t, desc)

	_, prev := previewWithDirectory(t, e)
	if ev := prev.event("mv"); len(ev.FormatMissing) != 1 || ev.FormatMissing[0] != "奇菱科技：缺 交通" {
		t.Errorf("format problems should name the vendor: %v", ev.FormatMissing)
	}
	_, out, _ := postSend(t, e, "")
	if len(mailer.sent) != 0 || len(notices.rows) != 0 || len(out.Incomplete) != 1 {
		t.Errorf("sent=%d incomplete=%+v", len(mailer.sent), out.Incomplete)
	}
}

func TestMultiVendor_ExtraTestMailboxDoesNotBlockSending(t *testing.T) {
	// 1 name, 2 mailboxes: fine — every mailbox identifies someone by itself.
	desc := vendor("台灣瑞環", "0900-1200", tainan1, "夏宗瑜", zoeyBox+"\n"+testBox)
	e, _, mailer := multiEnv(t, desc)
	_, out, _ := postSend(t, e, "")
	if len(out.Incomplete) != 0 || len(mailer.sent) != 1 {
		t.Errorf("incomplete=%+v sent=%d", out.Incomplete, len(mailer.sent))
	}
}
