package controller

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/port"
	"github.com/anthropics/mdm-server/internal/service/tripcar"
)

// noticeMailer is the part of the SMTP sender the notice needs.
type noticeMailer interface {
	Send(ctx context.Context, to, subject, htmlBody string) error
	Enabled() bool
}

// maxNoticesPerRequest bounds one "send" press; anything beyond it is left
// for the next press (the log makes that safe).
const maxNoticesPerRequest = 100

// WithNotices turns on mailing and the already-notified log. Without it the
// preview works as before and the send endpoint answers 503.
func (c *GoogleCalendarController) WithNotices(repo port.TripCarNoticeRepository, mailer noticeMailer) *GoogleCalendarController {
	c.notices, c.mailer = repo, mailer
	return c
}

func noticeKey(calendarID, eventID, eventStart, email string) string {
	return calendarID + "\x00" + eventID + "\x00" + eventStart + "\x00" + strings.ToLower(strings.TrimSpace(email))
}

// annotateNotices marks each candidate with what the log says about it.
func (c *GoogleCalendarController) annotateNotices(ctx context.Context, events []previewEvent, now time.Time) {
	if c.notices == nil || len(events) == 0 {
		return
	}
	logged, err := c.notices.ListFromDate(ctx, calendarDay(now).AddDate(0, 0, -1).Format("2006-01-02"))
	if err != nil {
		// Not fatal: sending still checks the log atomically before every mail.
		log.Printf("[google-calendar] preview: reading notice log failed: %v", err)
		return
	}
	byKey := make(map[string]domain.TripCarNotice, len(logged))
	for _, n := range logged {
		byKey[noticeKey(n.CalendarID, n.EventID, n.EventStart, n.RecipientEmail)] = n
	}
	for i := range events {
		ev := &events[i]
		for j := range ev.Candidates {
			cand := &ev.Candidates[j]
			if cand.Email == "" {
				continue
			}
			if n, ok := byKey[noticeKey(ev.CalendarID, ev.ID, ev.Start, cand.Email)]; ok {
				cand.NoticeStatus, cand.NoticeError = n.Status, n.Error
				if n.SentAt != nil {
					cand.NoticeSentAt = n.SentAt.In(taipei).Format(time.RFC3339)
				}
			}
		}
	}
}

// incompleteEvent is a due trip that was skipped for missing required fields.
type incompleteEvent struct {
	CalendarID string   `json:"calendar_id"`
	EventID    string   `json:"event_id"`
	Summary    string   `json:"summary"`
	Missing    []string `json:"missing"`
}

type noticeResult struct {
	CalendarID string `json:"calendar_id"`
	EventID    string `json:"event_id"`
	Summary    string `json:"summary"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	// Result: sent, already_sent (logged earlier or being sent right now), failed.
	Result string `json:"result"`
	Error  string `json:"error,omitempty"`
}

// handleSendNotices godoc
// @Summary 寄出到期的出差借車通知（sys_admin）
// @Description 只寄給「已比對到目錄、確定有駕照」的出發前 7 天內行程的出差人員；每人每個行程只會寄一次（已通知紀錄）。可用 calendar_id + event_id 只寄單一活動。
// @Tags Settings
// @Accept json
// @Produce json
// @Security BearerAuth
// @Router /api/settings/google-calendar/notices/send [post]
func (c *GoogleCalendarController) handleSendNotices(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireSysAdmin(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var body struct {
		CalendarID string `json:"calendar_id"`
		EventID    string `json:"event_id"`
	}
	if raw, _ := io.ReadAll(io.LimitReader(r.Body, 8<<10)); len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
	}
	if (body.CalendarID == "") != (body.EventID == "") {
		writeError(w, http.StatusBadRequest, "calendar_id 與 event_id 要一起提供")
		return
	}
	if c.notices == nil || c.mailer == nil {
		writeError(w, http.StatusServiceUnavailable, "寄信功能未啟用")
		return
	}
	if !c.mailer.Enabled() {
		writeError(w, http.StatusConflict, "尚未設定郵件伺服器（SMTP），請先到「郵件」設定")
		return
	}

	res, perr := c.loadPreview(r.Context(), tripcar.NoticeLeadDays+1)
	if perr != nil {
		writeError(w, perr.status, perr.msg)
		return
	}
	// Never mail from a half-known picture: without the directory we can't tell
	// who has a license, and a calendar that failed to read may hide events.
	if cfg, _ := res.Directory["configured"].(bool); !cfg {
		writeError(w, http.StatusConflict, "尚未設定人員目錄，無法確認誰有駕照")
		return
	}
	if msg, _ := res.Directory["error"].(string); msg != "" {
		writeError(w, http.StatusBadGateway, "人員目錄讀取失敗："+msg)
		return
	}
	c.annotateNotices(r.Context(), res.Events, c.now())

	results := []noticeResult{}
	incomplete := []incompleteEvent{}
	matched, truncated := false, false
	// Mark* run detached from the request: if the admin closes the page after
	// the mail left, the log must still say "sent", or it would go out again
	// once the pending claim expires.
	bg := context.WithoutCancel(r.Context())
loop:
	for i := range res.Events {
		ev := &res.Events[i]
		if body.EventID != "" && (ev.CalendarID != body.CalendarID || ev.ID != body.EventID) {
			continue
		}
		matched = true
		if !ev.TargetLocation || !ev.NoticeDue {
			continue
		}
		start, ok := eventStart(ev.Event)
		if !ok {
			continue
		}
		// The calendar format is mandatory: a half-filled event is reported to
		// the admin to fix, not mailed about on a guess.
		if len(ev.FormatMissing) > 0 {
			incomplete = append(incomplete, incompleteEvent{CalendarID: ev.CalendarID, EventID: ev.ID, Summary: ev.Summary, Missing: ev.FormatMissing})
			continue
		}
		for _, cand := range ev.Candidates {
			if cand.Decision != string(tripcar.Notify) || cand.Email == "" {
				continue
			}
			out := noticeResult{CalendarID: ev.CalendarID, EventID: ev.ID, Summary: ev.Summary, Name: firstNonEmpty(cand.Name, cand.DisplayName), Email: cand.Email}
			if cand.NoticeStatus == domain.NoticeSent {
				out.Result = "already_sent"
				results = append(results, out)
				continue
			}
			if sentCount(results) >= maxNoticesPerRequest {
				truncated = true
				break loop
			}
			id, claimed, err := c.notices.Claim(r.Context(), &domain.TripCarNotice{
				CalendarID: ev.CalendarID, EventID: ev.ID, EventStart: ev.Start, EventSummary: ev.Summary,
				RecipientEmail: cand.Email, RecipientAccount: cand.Account,
			}, claims.UserID)
			if err != nil {
				log.Printf("[trip-notice] claim failed: %v", err)
				out.Result, out.Error = "failed", "無法寫入通知紀錄"
				results = append(results, out)
				continue
			}
			if !claimed {
				out.Result = "already_sent"
				results = append(results, out)
				continue
			}
			subject, html, err := tripcar.RenderNotice(tripcar.NoticeData{
				Recipient: firstNonEmpty(cand.Name, cand.DisplayName, cand.Account),
				Summary:   ev.Summary, Location: ev.LocationUsed,
				Start: tripcar.StartLabel(start, ev.AllDay, taipei), Time: ev.Trip.Time, Transport: ev.Trip.Transport,
				DaysUntil: ev.DaysUntil,
			})
			if err == nil {
				err = c.mailer.Send(r.Context(), cand.Email, subject, html)
			}
			if err != nil {
				reason := truncateRunes(err.Error(), 300)
				log.Printf("[trip-notice] send failed event=%s to=%s: %v", ev.ID, cand.Email, err)
				if merr := c.notices.MarkFailed(bg, id, reason); merr != nil {
					log.Printf("[trip-notice] marking failed: %v", merr)
				}
				out.Result, out.Error = "failed", reason
			} else {
				if merr := c.notices.MarkSent(bg, id); merr != nil {
					log.Printf("[trip-notice] mail sent but marking sent failed (id=%d): %v", id, merr)
				}
				out.Result = "sent"
			}
			results = append(results, out)
		}
	}
	if body.EventID != "" && !matched {
		writeError(w, http.StatusNotFound, "找不到這個活動（可能已不在近期範圍內）")
		return
	}

	counts := map[string]int{"sent": 0, "already_sent": 0, "failed": 0}
	for _, o := range results {
		counts[o.Result]++
	}
	log.Printf("[trip-notice] send by user=%s sent=%d already=%d failed=%d", claims.UserID, counts["sent"], counts["already_sent"], counts["failed"])
	writeJSON(w, map[string]interface{}{"results": results, "counts": counts, "truncated": truncated, "incomplete": incomplete, "calendars": res.Calendars})
}

func sentCount(rs []noticeResult) int {
	n := 0
	for _, r := range rs {
		if r.Result != "already_sent" {
			n++
		}
	}
	return n
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
