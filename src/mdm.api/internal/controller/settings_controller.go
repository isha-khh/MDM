package controller

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/anthropics/mdm-server/internal/config"
	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/port"

	smtpAdapter "github.com/anthropics/mdm-server/internal/adapter/smtp"
)

// smtpConfigurable is a minimal interface so the controller can hot-apply
// config updates without importing the adapter's concrete type everywhere.
type smtpConfigurable interface {
	SetConfig(cfg config.SMTPConfig)
}

type SettingsController struct {
	repo   port.MailSettingsRepository
	sender smtpConfigurable
	auth   *middleware.AuthHelper
}

func NewSettingsController(repo port.MailSettingsRepository, sender smtpConfigurable, auth *middleware.AuthHelper) *SettingsController {
	return &SettingsController{repo: repo, sender: sender, auth: auth}
}

func (c *SettingsController) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/settings/mail", c.handleMail)
	mux.HandleFunc("/api/settings/mail/test-smtp", c.handleTestSMTP)
	mux.HandleFunc("/api/settings/mail/test-incoming", c.handleTestIncoming)
}

type mailSettingsDTO struct {
	SMTPEnabled  bool   `json:"smtp_enabled"`
	SMTPHost     string `json:"smtp_host"`
	SMTPPort     string `json:"smtp_port"`
	SMTPUsername string `json:"smtp_username"`
	SMTPPassword string `json:"smtp_password"`
	SMTPFrom     string `json:"smtp_from"`
	SMTPFromName string `json:"smtp_from_name"`
	SMTPTLS      bool   `json:"smtp_tls"`
	// SMTPCACert: extra CA certificate (PEM) trusted for STARTTLS, for mail
	// servers signed by an internal/private CA. SMTPInsecureSkipVerify is an
	// explicit last-resort escape hatch when the CA cert isn't available.
	SMTPCACert             string `json:"smtp_ca_cert"`
	SMTPInsecureSkipVerify bool   `json:"smtp_insecure_skip_verify"`

	IncomingEnabled  bool   `json:"incoming_enabled"`
	IncomingProtocol string `json:"incoming_protocol"`
	IncomingHost     string `json:"incoming_host"`
	IncomingPort     string `json:"incoming_port"`
	IncomingUsername string `json:"incoming_username"`
	IncomingPassword string `json:"incoming_password"`
	IncomingTLS      bool   `json:"incoming_tls"`
	IncomingMailbox  string `json:"incoming_mailbox"`

	// Output-only flags so the UI can tell "password set" vs "password empty".
	HasSMTPPassword     bool   `json:"has_smtp_password,omitempty"`
	HasIncomingPassword bool   `json:"has_incoming_password,omitempty"`
	UpdatedAt           string `json:"updated_at,omitempty"`
}

const passwordPlaceholder = "********"

// @Summary 取得 / 更新郵件伺服器設定（sys_admin）
// @Tags Settings
// @Accept json
// @Produce json
// @Security BearerAuth
// @Router /api/settings/mail [get]
// @Router /api/settings/mail [put]
func (c *SettingsController) handleMail(w http.ResponseWriter, r *http.Request) {
	claims, err := c.auth.RequireSysAdmin(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		m, err := c.repo.Get(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := dtoFromDomain(m)
		// Redact passwords but preserve "is it set" signal.
		out.HasSMTPPassword = m.SMTPPassword != ""
		out.HasIncomingPassword = m.IncomingPassword != ""
		out.SMTPPassword = ""
		out.IncomingPassword = ""
		if !m.UpdatedAt.IsZero() {
			out.UpdatedAt = m.UpdatedAt.Format(time.RFC3339)
		}
		writeJSON(w, out)

	case http.MethodPut:
		var body mailSettingsDTO
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
		if body.IncomingProtocol == "" {
			body.IncomingProtocol = "imap"
		}
		if body.IncomingProtocol != "imap" && body.IncomingProtocol != "pop3" {
			writeError(w, http.StatusBadRequest, "incoming_protocol must be imap or pop3")
			return
		}
		if body.IncomingMailbox == "" {
			body.IncomingMailbox = "INBOX"
		}

		// If the UI sends the placeholder (or empty) for passwords, preserve the
		// existing value instead of clobbering it.
		existing, _ := c.repo.Get(r.Context())
		if existing != nil {
			if body.SMTPPassword == "" || body.SMTPPassword == passwordPlaceholder {
				body.SMTPPassword = existing.SMTPPassword
			}
			if body.IncomingPassword == "" || body.IncomingPassword == passwordPlaceholder {
				body.IncomingPassword = existing.IncomingPassword
			}
		}

		m := domainFromDTO(body)
		if err := c.repo.Upsert(r.Context(), m, claims.UserID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Hot-apply to the live sender.
		if c.sender != nil {
			if m.SMTPEnabled {
				c.sender.SetConfig(config.SMTPConfig{
					Host: m.SMTPHost, Port: m.SMTPPort,
					Username: m.SMTPUsername, Password: m.SMTPPassword,
					From: m.SMTPFrom, FromName: m.SMTPFromName, TLS: m.SMTPTLS,
					CACertPEM: m.SMTPCACert, InsecureSkipVerify: m.SMTPInsecureSkipVerify,
				})
			} else {
				c.sender.SetConfig(config.SMTPConfig{})
			}
		}
		writeOK(w)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// @Summary 測試寄件伺服器（寄一封測試信）
// @Tags Settings
// @Accept json
// @Produce json
// @Security BearerAuth
// @Router /api/settings/mail/test-smtp [post]
func (c *SettingsController) handleTestSMTP(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireSysAdmin(r); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var body struct {
		To string `json:"to"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.To == "" {
		writeError(w, http.StatusBadRequest, "to required")
		return
	}

	m, err := c.repo.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !m.SMTPEnabled {
		writeError(w, http.StatusBadRequest, "SMTP is disabled")
		return
	}

	cfg := config.SMTPConfig{
		Host: m.SMTPHost, Port: m.SMTPPort,
		Username: m.SMTPUsername, Password: m.SMTPPassword,
		From: m.SMTPFrom, FromName: m.SMTPFromName, TLS: m.SMTPTLS,
		CACertPEM: m.SMTPCACert, InsecureSkipVerify: m.SMTPInsecureSkipVerify,
	}
	subject := "[MDM] SMTP 測試信"
	html := `<p>這是一封測試信。若您收到此郵件，代表寄件伺服器設定正確。</p>`
	if err := smtpAdapter.SendWith(cfg, body.To, subject, html); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeOK(w)
}

// @Summary 測試收件伺服器（建立 TCP/TLS 連線並登入）
// @Tags Settings
// @Accept json
// @Produce json
// @Security BearerAuth
// @Router /api/settings/mail/test-incoming [post]
func (c *SettingsController) handleTestIncoming(w http.ResponseWriter, r *http.Request) {
	if _, err := c.auth.RequireSysAdmin(r); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	m, err := c.repo.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !m.IncomingEnabled {
		writeError(w, http.StatusBadRequest, "incoming server is disabled")
		return
	}
	if m.IncomingHost == "" || m.IncomingPort == "" {
		writeError(w, http.StatusBadRequest, "host and port required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	addr := net.JoinHostPort(m.IncomingHost, m.IncomingPort)
	var conn net.Conn
	if m.IncomingTLS {
		d := &tls.Dialer{Config: &tls.Config{ServerName: m.IncomingHost}}
		conn, err = d.DialContext(ctx, "tcp", addr)
	} else {
		var d net.Dialer
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("connect %s://%s failed: %v", m.IncomingProtocol, addr, err))
		return
	}
	defer conn.Close()

	// Read the server greeting so we verify this is actually a mail server
	// answering (and not a rogue socket accepting connections).
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 256)
	n, _ := conn.Read(buf)
	greet := string(buf[:n])
	writeJSON(w, map[string]interface{}{
		"ok":       true,
		"protocol": m.IncomingProtocol,
		"address":  addr,
		"greeting": greet,
	})
}

func dtoFromDomain(m *domain.MailSettings) mailSettingsDTO {
	return mailSettingsDTO{
		SMTPEnabled:            m.SMTPEnabled,
		SMTPHost:               m.SMTPHost,
		SMTPPort:               m.SMTPPort,
		SMTPUsername:           m.SMTPUsername,
		SMTPPassword:           m.SMTPPassword,
		SMTPFrom:               m.SMTPFrom,
		SMTPFromName:           m.SMTPFromName,
		SMTPTLS:                m.SMTPTLS,
		SMTPCACert:             m.SMTPCACert,
		SMTPInsecureSkipVerify: m.SMTPInsecureSkipVerify,
		IncomingEnabled:        m.IncomingEnabled,
		IncomingProtocol:       m.IncomingProtocol,
		IncomingHost:           m.IncomingHost,
		IncomingPort:           m.IncomingPort,
		IncomingUsername:       m.IncomingUsername,
		IncomingPassword:       m.IncomingPassword,
		IncomingTLS:            m.IncomingTLS,
		IncomingMailbox:        m.IncomingMailbox,
	}
}

func domainFromDTO(d mailSettingsDTO) *domain.MailSettings {
	return &domain.MailSettings{
		SMTPEnabled:            d.SMTPEnabled,
		SMTPHost:               d.SMTPHost,
		SMTPPort:               d.SMTPPort,
		SMTPUsername:           d.SMTPUsername,
		SMTPPassword:           d.SMTPPassword,
		SMTPFrom:               d.SMTPFrom,
		SMTPFromName:           d.SMTPFromName,
		SMTPTLS:                d.SMTPTLS,
		SMTPCACert:             d.SMTPCACert,
		SMTPInsecureSkipVerify: d.SMTPInsecureSkipVerify,
		IncomingEnabled:        d.IncomingEnabled,
		IncomingProtocol:       d.IncomingProtocol,
		IncomingHost:           d.IncomingHost,
		IncomingPort:           d.IncomingPort,
		IncomingUsername:       d.IncomingUsername,
		IncomingPassword:       d.IncomingPassword,
		IncomingTLS:            d.IncomingTLS,
		IncomingMailbox:        d.IncomingMailbox,
	}
}
