package service

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"log"

	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/port"
)

//go:embed templates/*.html
var templateFS embed.FS

var emailTemplates *template.Template

func init() {
	var err error
	emailTemplates, err = template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		log.Printf("[notify] failed to parse email templates: %v", err)
	}
}

// NotifyService handles notifications (email) and records them for audit.
type NotifyService struct {
	sender   port.EmailSender        // nil if SMTP not configured
	notifRepo port.NotificationRepository
	userRepo  port.UserRepository
}

func NewNotifyService(sender port.EmailSender, notifRepo port.NotificationRepository, userRepo port.UserRepository) *NotifyService {
	return &NotifyService{sender: sender, notifRepo: notifRepo, userRepo: userRepo}
}

// RentalNotifyData holds data for rental email templates.
type RentalNotifyData struct {
	RentalNumber int
	BorrowerName string
	ApproverName string
	DeviceNames  []string
	Purpose      string
	ExpectedReturn string
	Notes        string
	OverdueDays  int
	Checklist    string
	ReturnNotes  string
}

// SendRentalRequest sends email to approver/custodian when a new rental is created.
func (s *NotifyService) SendRentalRequest(ctx context.Context, data RentalNotifyData, recipientEmail string) {
	s.sendNotification(ctx, "rental_request", recipientEmail, data.RentalNumber,
		fmt.Sprintf("[MDM] 新租借申請待核准 — #%d", data.RentalNumber),
		"rental_request.html", data)
}

// SendRentalApproved sends email to borrower when rental is approved.
func (s *NotifyService) SendRentalApproved(ctx context.Context, data RentalNotifyData, recipientEmail string) {
	s.sendNotification(ctx, "rental_approved", recipientEmail, data.RentalNumber,
		fmt.Sprintf("[MDM] 您的租借申請已核准 — #%d", data.RentalNumber),
		"rental_approved.html", data)
}

// SendRentalRejected sends email to borrower when rental is rejected.
func (s *NotifyService) SendRentalRejected(ctx context.Context, data RentalNotifyData, recipientEmail string) {
	s.sendNotification(ctx, "rental_rejected", recipientEmail, data.RentalNumber,
		fmt.Sprintf("[MDM] 您的租借申請已拒絕 — #%d", data.RentalNumber),
		"rental_rejected.html", data)
}

// SendRentalActivated sends email to borrower when devices are handed out.
func (s *NotifyService) SendRentalActivated(ctx context.Context, data RentalNotifyData, recipientEmail string) {
	s.sendNotification(ctx, "rental_activated", recipientEmail, data.RentalNumber,
		fmt.Sprintf("[MDM] 裝置已借出 — #%d", data.RentalNumber),
		"rental_activated.html", data)
}

// SendRentalOverdue sends email to borrower + custodian when overdue.
func (s *NotifyService) SendRentalOverdue(ctx context.Context, data RentalNotifyData, recipientEmail string) {
	s.sendNotification(ctx, "rental_overdue", recipientEmail, data.RentalNumber,
		fmt.Sprintf("[MDM] 租借逾期提醒 — #%d", data.RentalNumber),
		"rental_overdue.html", data)
}

// SendRentalReturned sends email to custodian when devices are returned.
func (s *NotifyService) SendRentalReturned(ctx context.Context, data RentalNotifyData, recipientEmail string) {
	s.sendNotification(ctx, "rental_returned", recipientEmail, data.RentalNumber,
		fmt.Sprintf("[MDM] 裝置已歸還 — #%d", data.RentalNumber),
		"rental_returned.html", data)
}

// SendRentalPendingVerification sends email to custodian when the borrower
// has submitted stage-1 of the two-stage return (see RentalController's
// "submit-return"), asking them to verify and complete stage 2.
func (s *NotifyService) SendRentalPendingVerification(ctx context.Context, data RentalNotifyData, recipientEmail string) {
	s.sendNotification(ctx, "rental_pending_verification", recipientEmail, data.RentalNumber,
		fmt.Sprintf("[MDM] 有一筆待核對的歸還 — #%d", data.RentalNumber),
		"rental_pending_verification.html", data)
}

func (s *NotifyService) sendNotification(ctx context.Context, event, recipient string, rentalNumber int, subject, tmplName string, data interface{}) {
	// Render template
	var body bytes.Buffer
	if emailTemplates != nil {
		if err := emailTemplates.ExecuteTemplate(&body, tmplName, data); err != nil {
			log.Printf("[notify] template %s error: %v", tmplName, err)
			body.WriteString(fmt.Sprintf("租借單號: #%d", rentalNumber))
		}
	} else {
		body.WriteString(fmt.Sprintf("租借單號: #%d", rentalNumber))
	}

	// Record notification
	notif := &domain.Notification{
		Type:        "email",
		Event:       event,
		Recipient:   recipient,
		Subject:     subject,
		Body:        body.String(),
		Status:      "pending",
		ReferenceID: fmt.Sprint(rentalNumber),
	}
	id, err := s.notifRepo.Create(ctx, notif)
	if err != nil {
		log.Printf("[notify] failed to record notification: %v", err)
		return
	}

	// Send email if SMTP is configured
	if s.sender == nil || recipient == "" {
		log.Printf("[notify] skipping email (no SMTP or no recipient): event=%s", event)
		s.notifRepo.UpdateStatus(ctx, id, "failed", "no SMTP configured or no recipient")
		return
	}

	if err := s.sender.Send(ctx, recipient, subject, body.String()); err != nil {
		s.notifRepo.UpdateStatus(ctx, id, "failed", err.Error())
		return
	}
	s.notifRepo.UpdateStatus(ctx, id, "sent", "")
}
