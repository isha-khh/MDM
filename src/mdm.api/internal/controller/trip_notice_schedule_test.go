package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/mdm-server/internal/adapter/google"
	"github.com/anthropics/mdm-server/internal/domain"
)

// fakeSchedule keeps the SQL claim rules: ClaimRun succeeds only when enabled
// and today isn't taken yet.
type fakeSchedule struct {
	s        domain.TripNoticeSchedule
	claimErr error
	released []string
	claims   int
}

func (f *fakeSchedule) Get(context.Context) (*domain.TripNoticeSchedule, error) {
	c := f.s
	return &c, nil
}
func (f *fakeSchedule) Save(_ context.Context, enabled bool, hour int, _ string) error {
	f.s.Enabled, f.s.SendHour = enabled, hour
	return nil
}
func (f *fakeSchedule) ClaimRun(_ context.Context, today string) (bool, error) {
	if f.claimErr != nil {
		return false, f.claimErr
	}
	if !f.s.Enabled || f.s.LastRunDate == today {
		return false, nil
	}
	f.s.LastRunDate = today
	f.claims++
	return true, nil
}
func (f *fakeSchedule) ReleaseRun(_ context.Context, today string) error {
	if f.s.LastRunDate == today {
		f.s.LastRunDate = ""
	}
	f.released = append(f.released, today)
	return nil
}
func (f *fakeSchedule) RecordRun(_ context.Context, summary string) error {
	f.s.LastRunSummary = summary
	return nil
}

type fakeSender struct {
	calls []string // the `by` of each call
	rep   *SendReport
	err   *previewError
}

func (f *fakeSender) SendDueNotices(_ context.Context, by, cal, ev string) (*SendReport, *previewError) {
	f.calls = append(f.calls, by)
	if f.err != nil {
		return nil, f.err
	}
	if f.rep != nil {
		return f.rep, nil
	}
	return &SendReport{Counts: map[string]int{"sent": 1, "already_sent": 0, "failed": 0}}, nil
}

// clock returns a scheduler whose time the test controls (given in UTC).
func newSched(enabled bool, hour int, at time.Time) (*TripNoticeScheduler, *fakeSchedule, *fakeSender, *time.Time) {
	repo := &fakeSchedule{s: domain.TripNoticeSchedule{Enabled: enabled, SendHour: hour}}
	snd := &fakeSender{}
	now := at
	s := NewTripNoticeScheduler(repo, snd)
	s.now = func() time.Time { return now }
	return s, repo, snd, &now
}

func utc(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, time.UTC)
}

func TestScheduler_OffMeansNothingHappens(t *testing.T) {
	s, repo, snd, _ := newSched(false, 9, utc(2026, 10, 20, 3, 0)) // 11:00 Taipei
	s.Tick(context.Background())
	if len(snd.calls) != 0 || repo.claims != 0 {
		t.Errorf("a disabled schedule must not send or even claim the day: %v", snd.calls)
	}
}

func TestScheduler_WaitsForTheSendHourInTaipeiTime(t *testing.T) {
	s, _, snd, now := newSched(true, 9, utc(2026, 10, 20, 0, 59)) // 08:59 Taipei
	s.Tick(context.Background())
	if len(snd.calls) != 0 {
		t.Fatal("ran before the send hour")
	}
	*now = utc(2026, 10, 20, 1, 0) // 09:00 Taipei
	s.Tick(context.Background())
	if len(snd.calls) != 1 || snd.calls[0] != scheduleSystemUser {
		t.Errorf("calls %v", snd.calls)
	}
}

func TestScheduler_OncePerDay(t *testing.T) {
	s, repo, snd, now := newSched(true, 9, utc(2026, 10, 20, 1, 0))
	for i := 0; i < 5; i++ { // the 10-minute ticks of the same day
		*now = now.Add(10 * time.Minute)
		s.Tick(context.Background())
	}
	if len(snd.calls) != 1 {
		t.Errorf("ran %d times in one day", len(snd.calls))
	}
	if !strings.Contains(repo.s.LastRunSummary, "寄出 1 封") {
		t.Errorf("summary: %q", repo.s.LastRunSummary)
	}
	*now = utc(2026, 10, 21, 1, 0) // next day
	s.Tick(context.Background())
	if len(snd.calls) != 2 {
		t.Errorf("should run again the next day: %d", len(snd.calls))
	}
}

func TestScheduler_UsesTheTaipeiDateNotUTC(t *testing.T) {
	// 17:00 UTC on Oct 19 is 01:00 on Oct 20 in Taipei: that is already "today's" run.
	s, repo, snd, now := newSched(true, 0, utc(2026, 10, 19, 17, 0))
	s.Tick(context.Background())
	if len(snd.calls) != 1 || repo.s.LastRunDate != "2026-10-20" {
		t.Fatalf("calls=%d date=%q", len(snd.calls), repo.s.LastRunDate)
	}
	*now = utc(2026, 10, 20, 15, 59) // 23:59 Taipei, still Oct 20
	s.Tick(context.Background())
	if len(snd.calls) != 1 {
		t.Error("same Taipei day, must not run again")
	}
	*now = utc(2026, 10, 20, 16, 0) // 00:00 Oct 21 Taipei
	s.Tick(context.Background())
	if len(snd.calls) != 2 {
		t.Error("new Taipei day should run")
	}
}

func TestScheduler_FailedRunIsRetriedAtTheNextTick(t *testing.T) {
	s, repo, snd, now := newSched(true, 9, utc(2026, 10, 20, 1, 0))
	snd.err = &previewError{502, "Google 暫時無法連線"}
	s.Tick(context.Background())
	if len(snd.calls) != 1 || repo.s.LastRunDate != "" || !strings.Contains(repo.s.LastRunSummary, "Google 暫時無法連線") {
		t.Fatalf("a failed run must give the day back: calls=%d date=%q summary=%q", len(snd.calls), repo.s.LastRunDate, repo.s.LastRunSummary)
	}
	snd.err = nil
	*now = now.Add(scheduleTickInterval)
	s.Tick(context.Background())
	if len(snd.calls) != 2 || repo.s.LastRunDate != "2026-10-20" {
		t.Errorf("retry: calls=%d date=%q", len(snd.calls), repo.s.LastRunDate)
	}
	*now = now.Add(scheduleTickInterval)
	s.Tick(context.Background())
	if len(snd.calls) != 2 {
		t.Error("after a successful run it must stop for the day")
	}
}

func TestScheduler_TruncatedRunContinuesAtTheNextTick(t *testing.T) {
	s, repo, snd, now := newSched(true, 9, utc(2026, 10, 20, 1, 0))
	snd.rep = &SendReport{Counts: map[string]int{"sent": 100}, Truncated: true}
	s.Tick(context.Background())
	if repo.s.LastRunDate != "" {
		t.Fatal("more is waiting; the day must stay open")
	}
	snd.rep = &SendReport{Counts: map[string]int{"sent": 5}}
	*now = now.Add(scheduleTickInterval)
	s.Tick(context.Background())
	*now = now.Add(scheduleTickInterval)
	s.Tick(context.Background())
	if len(snd.calls) != 2 {
		t.Errorf("calls %d", len(snd.calls))
	}
}

func TestScheduler_LosingTheClaimOrAFailingStoreSendsNothing(t *testing.T) {
	s, repo, snd, _ := newSched(true, 9, utc(2026, 10, 20, 1, 0))
	repo.s.LastRunDate = "2026-10-20" // another instance already took today
	s.Tick(context.Background())
	repo.s.LastRunDate = ""
	repo.claimErr = errors.New("db down")
	s.Tick(context.Background())
	if len(snd.calls) != 0 {
		t.Errorf("sent without owning the day's run: %v", snd.calls)
	}
}

// ---- the schedule driving the real sender: the "notified once, never again" guarantee ----

func TestScheduler_NotifiedPeopleAreNeverMailedAgain(t *testing.T) {
	e, notices, mailer := noticeEnv(t)
	repo := &fakeSchedule{s: domain.TripNoticeSchedule{Enabled: true, SendHour: 9}}
	s := NewTripNoticeScheduler(repo, e.c)
	day := utc(2026, 10, 20, 1, 0) // 09:00 Taipei
	s.now = func() time.Time { return day }
	e.c.now = func() time.Time { return day }
	e.g.events["bip@group"] = nil
	e.g.events["ida@group"] = []google.Event{{ID: "t", Summary: "進廠(臺南分局)", Start: "2026-10-27", AllDay: true, Description: tripDesc("臺南", "羅羿昇", "夏宗瑜")}}
	mailer.failFor["zoey.xia@mail.isha.org.tw"] = errors.New("smtp: 451 try again later")

	// Day 1: one delivered, one failed.
	s.Tick(context.Background())
	if got := mailer.recipients(); len(got) != 1 || !strings.Contains(got[0], "yishengluo1006") {
		t.Fatalf("day 1: %v", got)
	}
	if !strings.Contains(repo.s.LastRunSummary, "寄出 1 封") || !strings.Contains(repo.s.LastRunSummary, "失敗 1 封") {
		t.Errorf("summary: %q", repo.s.LastRunSummary)
	}

	// Day 2: the failed one is retried and delivered; the delivered one is not mailed again.
	delete(mailer.failFor, "zoey.xia@mail.isha.org.tw")
	day = utc(2026, 10, 21, 1, 0)
	s.Tick(context.Background())
	got := mailer.recipients()
	if len(got) != 2 || !strings.Contains(got[1], "zoey.xia") {
		t.Fatalf("day 2: %v", got)
	}

	// Every later day until the trip: nothing more goes out.
	for d := 22; d <= 26; d++ {
		day = utc(2026, 10, d, 1, 0)
		s.Tick(context.Background())
	}
	if len(mailer.sent) != 2 {
		t.Errorf("a notified person was mailed again: %v", mailer.recipients())
	}
	for _, n := range notices.rows {
		if n.Status != domain.NoticeSent {
			t.Errorf("log: %+v", n)
		}
	}
}

func TestSend_MovingTheTimeWithinTheSameDayDoesNotNotifyAgain(t *testing.T) {
	e, _, mailer := noticeEnv(t)
	e.g.events["bip@group"] = nil
	e.g.events["ida@group"] = []google.Event{{ID: "t", Summary: "進廠(臺南分局)", Start: "2026-10-23T10:00:00+08:00", Description: tripDesc("臺南", "羅羿昇")}}
	postSend(t, e, "")
	e.g.events["ida@group"][0].Start = "2026-10-23T13:30:00+08:00" // same day, later
	postSend(t, e, "")
	if len(mailer.sent) != 1 {
		t.Errorf("same-day time change re-mailed: %d", len(mailer.sent))
	}
	e.g.events["ida@group"][0].Start = "2026-10-24T13:30:00+08:00" // a different day
	postSend(t, e, "")
	if len(mailer.sent) != 2 || !strings.Contains(mailer.sent[1].subject, "2026-10-24") {
		t.Errorf("a different day should be announced: %d", len(mailer.sent))
	}
}

// ---- settings endpoint ----

const schedulePath = basePath + "/notices/schedule"

func scheduleEnv(t *testing.T) (*env, *fakeSchedule, *fakeMailer) {
	e, _, mailer := noticeEnv(t)
	repo := &fakeSchedule{s: domain.TripNoticeSchedule{SendHour: 9}}
	e.c.WithSchedule(repo)
	return e, repo, mailer
}

type scheduleResp struct {
	Enabled  bool     `json:"enabled"`
	SendHour int      `json:"send_hour"`
	Problems []string `json:"problems"`
}

func TestScheduleEndpoint_ReadAndUpdate(t *testing.T) {
	e, repo, _ := scheduleEnv(t)
	admin := tokenFor(t, "admin", "sys_admin")

	rec := do(e.mux, "GET", schedulePath, admin, "", nil)
	var st scheduleResp
	json.Unmarshal(rec.Body.Bytes(), &st)
	if rec.Code != 200 || st.Enabled || st.SendHour != 9 {
		t.Fatalf("default should be off at 9: %d %s", rec.Code, rec.Body)
	}

	rec = do(e.mux, "PUT", schedulePath, admin, "application/json", []byte(`{"enabled":true,"send_hour":8}`))
	json.Unmarshal(rec.Body.Bytes(), &st)
	if rec.Code != 200 || !repo.s.Enabled || repo.s.SendHour != 8 || !st.Enabled || st.SendHour != 8 {
		t.Errorf("%d %s stored %+v", rec.Code, rec.Body, repo.s)
	}
	if len(st.Problems) != 0 {
		t.Errorf("everything is set up in this env: %v", st.Problems)
	}
	// Hour 0 is valid (midnight), not "missing".
	if rec = do(e.mux, "PUT", schedulePath, admin, "application/json", []byte(`{"enabled":true,"send_hour":0}`)); rec.Code != 200 || repo.s.SendHour != 0 {
		t.Errorf("hour 0: %d %+v", rec.Code, repo.s)
	}
}

func TestScheduleEndpoint_RejectsBadInput(t *testing.T) {
	e, repo, _ := scheduleEnv(t)
	admin := tokenFor(t, "admin", "sys_admin")
	for name, body := range map[string]string{
		"hour 24": `{"enabled":true,"send_hour":24}`, "negative": `{"enabled":true,"send_hour":-1}`,
		"no hour": `{"enabled":true}`, "garbage": `nope`,
	} {
		if rec := do(e.mux, "PUT", schedulePath, admin, "application/json", []byte(body)); rec.Code != 400 {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if repo.s.Enabled || repo.s.SendHour != 9 {
		t.Errorf("rejected input changed the setting: %+v", repo.s)
	}
}

func TestScheduleEndpoint_ExplainsWhatWouldStopARun(t *testing.T) {
	e, _, mailer := scheduleEnv(t)
	mailer.disabled = true
	e.dir.err = ErrDirectoryNotConfigured
	rec := do(e.mux, "GET", schedulePath, tokenFor(t, "admin", "sys_admin"), "", nil)
	var st scheduleResp
	json.Unmarshal(rec.Body.Bytes(), &st)
	joined := strings.Join(st.Problems, "|")
	if !strings.Contains(joined, "SMTP") || !strings.Contains(joined, "人員目錄") {
		t.Errorf("problems: %v", st.Problems)
	}
}

func TestScheduleEndpoint_Access(t *testing.T) {
	e, repo, _ := scheduleEnv(t)
	for name, tok := range map[string]string{"anonymous": "", "ordinary user": tokenFor(t, "user", "")} {
		for _, m := range []string{"GET", "PUT"} {
			if rec := do(e.mux, m, schedulePath, tok, "application/json", []byte(`{"enabled":true,"send_hour":1}`)); rec.Code != 401 {
				t.Errorf("%s %s: %d", m, name, rec.Code)
			}
		}
	}
	if repo.s.Enabled {
		t.Error("an unauthorised request turned it on")
	}
	if rec := do(e.mux, "DELETE", schedulePath, tokenFor(t, "admin", "sys_admin"), "", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE: %d", rec.Code)
	}
	plain := previewEnv(t) // schedule never wired
	if rec := do(plain.mux, "GET", schedulePath, tokenFor(t, "admin", "sys_admin"), "", nil); rec.Code != 503 {
		t.Errorf("not wired: %d", rec.Code)
	}
}
