package tripcar

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"time"
)

// NoticeData is what the rental-car notice mail says.
type NoticeData struct {
	Recipient string // how the person is addressed (directory display name or the name in the event)
	Summary   string // event title
	Location  string // destination that triggered the notice
	Start     string // "2026-10-27" or "2026-10-27 08:00"
	Time      string // free-text 時間 line from the description, optional
	Transport string // free-text 交通 line, optional
	DaysUntil int
}

// NoticeSubject keeps the date in the subject so a person with several trips
// can tell the mails apart.
func NoticeSubject(d NoticeData) string {
	return fmt.Sprintf("[MDM] 出差借車提醒 — %s 出發（%s）", d.Start, oneLine(d.Location))
}

// oneLine collapses line breaks: subjects must never carry a newline, and a
// destination typed across lines in a calendar would otherwise break the
// header (the SMTP layer rejects it rather than sending).
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

var noticeTmpl = template.Must(template.New("notice").Parse(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family: Arial, sans-serif; max-width: 600px; margin: 0 auto; padding: 20px;">
  <h2 style="color: #1565c0;">出差借車提醒</h2>
  <p>{{.Recipient}} 您好，</p>
  <p>您預計於 <strong>{{.Start}}</strong>（{{.DaysUntil}} 天後）出差，行程如下。目的地需要開車，若您需要租借公務車，請盡早到 MDM 平台提出借車申請。</p>
  <table style="border-collapse: collapse; width: 100%; margin: 16px 0;">
    <tr><td style="padding: 8px; border: 1px solid #ddd; background: #f9f9f9; width: 120px;"><strong>行程</strong></td><td style="padding: 8px; border: 1px solid #ddd;">{{.Summary}}</td></tr>
    <tr><td style="padding: 8px; border: 1px solid #ddd; background: #f9f9f9;"><strong>地點</strong></td><td style="padding: 8px; border: 1px solid #ddd;">{{.Location}}</td></tr>
    <tr><td style="padding: 8px; border: 1px solid #ddd; background: #f9f9f9;"><strong>出發日</strong></td><td style="padding: 8px; border: 1px solid #ddd;">{{.Start}}</td></tr>
    {{if .Time}}<tr><td style="padding: 8px; border: 1px solid #ddd; background: #f9f9f9;"><strong>時間</strong></td><td style="padding: 8px; border: 1px solid #ddd;">{{.Time}}</td></tr>{{end}}
    {{if .Transport}}<tr><td style="padding: 8px; border: 1px solid #ddd; background: #f9f9f9;"><strong>交通</strong></td><td style="padding: 8px; border: 1px solid #ddd;">{{.Transport}}</td></tr>{{end}}
  </table>
  <p>若行程已有安排或不需要借車，請忽略此信。</p>
  <hr style="border: none; border-top: 1px solid #eee; margin: 20px 0;">
  <p style="color: #999; font-size: 12px;">此信件由 MDM 管理平台自動發送，請勿直接回覆。</p>
</body>
</html>`))

// RenderNotice returns the subject and HTML body. Every field comes from a
// calendar anyone with write access to it can edit, so the body is rendered
// with html/template (escaped) and the subject is flattened to one line.
func RenderNotice(d NoticeData) (subject, body string, err error) {
	d.Summary, d.Location, d.Time, d.Transport = oneLine(d.Summary), oneLine(d.Location), oneLine(d.Time), oneLine(d.Transport)
	d.Recipient = oneLine(d.Recipient)
	var buf bytes.Buffer
	if err := noticeTmpl.Execute(&buf, d); err != nil {
		return "", "", err
	}
	return NoticeSubject(d), buf.String(), nil
}

// StartLabel formats an event start for the mail: the date for all-day
// events, date and time otherwise, in the given zone.
func StartLabel(t time.Time, allDay bool, loc *time.Location) string {
	t = t.In(loc)
	if allDay {
		return t.Format("2006-01-02")
	}
	return t.Format("2006-01-02 15:04")
}
