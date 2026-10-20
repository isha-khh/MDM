package controller

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/anthropics/mdm-server/internal/port"
)

// scheduleSystemUser is what the log records as the sender of automatic runs.
const scheduleSystemUser = "system:schedule"

// scheduleTickInterval is how often the scheduler looks at the clock. The
// work happens at most once a day; the short tick just means a run that had
// to be given up (Google unreachable, say) is retried within minutes.
const scheduleTickInterval = 10 * time.Minute

// noticeSender is what the scheduler drives; *GoogleCalendarController is it.
type noticeSender interface {
	SendDueNotices(ctx context.Context, by, calendarID, eventID string) (*SendReport, *previewError)
}

// TripNoticeScheduler sends the due trip notices once a day, from the
// configured hour (Taipei time). Whatever was mailed is in the notice log, so
// a person who was notified is never mailed again however often this runs.
type TripNoticeScheduler struct {
	repo   port.TripNoticeScheduleRepository
	sender noticeSender
	now    func() time.Time
}

func NewTripNoticeScheduler(repo port.TripNoticeScheduleRepository, sender noticeSender) *TripNoticeScheduler {
	return &TripNoticeScheduler{repo: repo, sender: sender, now: time.Now}
}

// Start runs the loop until ctx is cancelled.
func (s *TripNoticeScheduler) Start(ctx context.Context) {
	go func() {
		log.Printf("[trip-notice] scheduler started, checking every %s", scheduleTickInterval)
		s.Tick(ctx)
		t := time.NewTicker(scheduleTickInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.Tick(ctx)
			}
		}
	}()
}

// Tick does one check; it sends only if the schedule is on, the send hour has
// come, and nobody has taken today's run yet.
func (s *TripNoticeScheduler) Tick(ctx context.Context) {
	cfg, err := s.repo.Get(ctx)
	if err != nil {
		log.Printf("[trip-notice] schedule: reading settings failed: %v", err)
		return
	}
	now := s.now().In(taipei)
	if !cfg.Enabled || now.Hour() < cfg.SendHour {
		return
	}
	today := now.Format("2006-01-02")
	claimed, err := s.repo.ClaimRun(ctx, today)
	if err != nil {
		log.Printf("[trip-notice] schedule: claiming today's run failed: %v", err)
		return
	}
	if !claimed {
		return // already ran today (here or on another instance)
	}
	bg := context.WithoutCancel(ctx)
	rep, perr := s.sender.SendDueNotices(ctx, scheduleSystemUser, "", "")
	if perr != nil {
		log.Printf("[trip-notice] schedule: run failed: %s", perr.msg)
		s.record(bg, "失敗，稍後重試："+perr.msg)
		if err := s.repo.ReleaseRun(bg, today); err != nil {
			log.Printf("[trip-notice] schedule: releasing today's run failed: %v", err)
		}
		return
	}
	s.record(bg, rep.Summary())
	if rep.Truncated {
		// More are waiting than one pass sends: carry on at the next tick.
		if err := s.repo.ReleaseRun(bg, today); err != nil {
			log.Printf("[trip-notice] schedule: releasing today's run failed: %v", err)
		}
	}
}

func (s *TripNoticeScheduler) record(ctx context.Context, summary string) {
	if err := s.repo.RecordRun(ctx, summary); err != nil {
		log.Printf("[trip-notice] schedule: recording the result failed: %v", err)
	}
}

// WithSchedule enables the schedule settings endpoint.
func (c *GoogleCalendarController) WithSchedule(repo port.TripNoticeScheduleRepository) *GoogleCalendarController {
	c.schedule = repo
	return c
}

type scheduleStatus struct {
	Enabled        bool   `json:"enabled"`
	SendHour       int    `json:"send_hour"`
	LastRunAt      string `json:"last_run_at,omitempty"`
	LastRunSummary string `json:"last_run_summary,omitempty"`
	// Ready says whether a run could actually send right now; Problems say why not.
	Problems []string `json:"problems"`
}

func (c *GoogleCalendarController) scheduleStatus(ctx context.Context) (*scheduleStatus, error) {
	cfg, err := c.schedule.Get(ctx)
	if err != nil {
		return nil, err
	}
	st := &scheduleStatus{Enabled: cfg.Enabled, SendHour: cfg.SendHour, LastRunSummary: cfg.LastRunSummary, Problems: []string{}}
	if cfg.LastRunAt != nil {
		st.LastRunAt = cfg.LastRunAt.In(taipei).Format(time.RFC3339)
	}
	if c.mailer == nil || !c.mailer.Enabled() {
		st.Problems = append(st.Problems, "尚未設定郵件伺服器（SMTP）")
	}
	if s, err := c.repo.Get(ctx); err == nil && (s.RefreshTokenEnc == "" || len(s.SelectedCalendarIDs) == 0) {
		st.Problems = append(st.Problems, "尚未連結 Google 帳號或尚未選擇日曆")
	}
	if c.directory == nil {
		st.Problems = append(st.Problems, "尚未設定人員目錄")
	} else if _, err := c.directory.Users(ctx); err != nil {
		st.Problems = append(st.Problems, "人員目錄無法使用："+err.Error())
	}
	return st, nil
}

// handleSchedule godoc
// @Summary 每日自動寄送出差借車通知的設定（sys_admin）
// @Description 開啟後，每天到了設定的時間（台灣時間）自動寄出到期的通知；已通知過的人不會再寄。預設關閉。
// @Tags Settings
// @Accept json
// @Produce json
// @Security BearerAuth
// @Router /api/settings/google-calendar/notices/schedule [get]
// @Router /api/settings/google-calendar/notices/schedule [put]
func (c *GoogleCalendarController) handleSchedule(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireSysAdmin(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if c.schedule == nil {
		writeError(w, http.StatusServiceUnavailable, "自動寄送未啟用")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		var body struct {
			Enabled  bool `json:"enabled"`
			SendHour *int `json:"send_hour"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&body); err != nil || body.SendHour == nil {
			writeError(w, http.StatusBadRequest, "需要 enabled 與 send_hour")
			return
		}
		if *body.SendHour < 0 || *body.SendHour > 23 {
			writeError(w, http.StatusBadRequest, "send_hour 需介於 0 到 23")
			return
		}
		if err := c.schedule.Save(r.Context(), body.Enabled, *body.SendHour, claims.UserID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		log.Printf("[trip-notice] schedule set by user=%s enabled=%v hour=%d", claims.UserID, body.Enabled, *body.SendHour)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	st, err := c.scheduleStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, st)
}
