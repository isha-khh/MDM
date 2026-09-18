package smtp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"sync"

	"github.com/anthropics/mdm-server/internal/config"
)

// sanitizeHeaderValue rejects a header value that could inject a second
// header or body (an attacker who controls `subject` or a display name
// embedding "\r\nBcc: ..." can otherwise smuggle extra SMTP headers — a
// textbook header injection). Reject-and-fail rather than silently
// stripping: a value that needed cleaning indicates the input didn't come
// from where the caller expected, and sending it anyway (even cleaned)
// risks masking that.
func sanitizeHeaderValue(v string) (string, error) {
	if strings.ContainsAny(v, "\r\n") {
		return "", errors.New("smtp: header value contains invalid newline characters")
	}
	return v, nil
}

// sanitizeEmailAddress validates that v is a well-formed RFC 5322 address —
// stronger than sanitizeHeaderValue's bare CRLF check — and returns just the
// bare address, discarding any display-name portion the caller may have
// embedded (which is itself a header-injection vector for the To:/From:
// envelope lines).
func sanitizeEmailAddress(v string) (string, error) {
	if strings.ContainsAny(v, "\r\n") {
		return "", errors.New("smtp: email address contains invalid newline characters")
	}
	addr, err := mail.ParseAddress(v)
	if err != nil {
		return "", fmt.Errorf("smtp: invalid email address: %w", err)
	}
	return addr.Address, nil
}

// Sender implements port.EmailSender via SMTP. It holds a mutable config so
// the settings controller can hot-reload credentials without a restart.
type Sender struct {
	mu      sync.RWMutex
	cfg     config.SMTPConfig
	enabled bool
}

// NewSender always returns a sender — it may be disabled until SetConfig is
// called with a valid configuration. Callers can safely treat a nil return
// from NewSender as "no SMTP configured"; callers that want hot-reload should
// keep the instance and call SetConfig.
func NewSender(cfg config.SMTPConfig) *Sender {
	s := &Sender{cfg: cfg}
	s.enabled = cfg.Host != "" && cfg.From != ""
	return s
}

// SetConfig replaces the active SMTP config. Passing a zero-value config with
// empty host/from disables the sender.
func (s *Sender) SetConfig(cfg config.SMTPConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
	s.enabled = cfg.Host != "" && cfg.From != ""
}

// Config returns a copy of the current config.
func (s *Sender) Config() config.SMTPConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Enabled reports whether the sender has a usable config.
func (s *Sender) Enabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

func (s *Sender) Send(_ context.Context, to string, subject string, htmlBody string) error {
	s.mu.RLock()
	cfg := s.cfg
	enabled := s.enabled
	s.mu.RUnlock()

	if !enabled {
		return errors.New("smtp: not configured")
	}
	return sendWith(cfg, to, subject, htmlBody)
}

// SendWith allows callers (e.g. settings test endpoint) to send with an ad-hoc
// config without mutating the registered sender.
func SendWith(cfg config.SMTPConfig, to, subject, htmlBody string) error {
	if cfg.Host == "" || cfg.From == "" {
		return errors.New("smtp: host and from are required")
	}
	return sendWith(cfg, to, subject, htmlBody)
}

// builtMessage is the result of buildMessage: the raw RFC 5322 message bytes
// plus the envelope from/to actually used to send it (post-sanitization).
type builtMessage struct {
	From string
	To   string
	Data []byte
}

// buildMessage assembles the raw SMTP message, including CRLF-injection
// sanitization and RFC 2047 header encoding for non-ASCII subject/display
// names. Pulled out of sendWith so it can be unit tested without a network
// round-trip.
func buildMessage(cfg config.SMTPConfig, to, subject, htmlBody string) (builtMessage, error) {
	// Validate every value that ends up on a header line — reject rather than
	// silently strip, since a value that needed cleaning means the input
	// didn't look like what we expected. `to`/`From` go through full RFC 5322
	// address validation (net/mail); free-text header values (subject, the
	// display name) only need the CRLF check since any other content is a
	// legal header value.
	cleanTo, err := sanitizeEmailAddress(to)
	if err != nil {
		return builtMessage{}, err
	}
	cleanFrom, err := sanitizeEmailAddress(cfg.From)
	if err != nil {
		return builtMessage{}, err
	}
	cleanSubject, err := sanitizeHeaderValue(subject)
	if err != nil {
		return builtMessage{}, err
	}
	cleanFromName, err := sanitizeHeaderValue(cfg.FromName)
	if err != nil {
		return builtMessage{}, err
	}

	// RFC 5322 header lines are ASCII-only; a raw UTF-8 subject/display name
	// (e.g. Chinese) renders as mojibake in most mail clients unless wrapped
	// in an RFC 2047 encoded-word. mime.QEncoding.Encode is a no-op for
	// already-ASCII input, so this is safe for English-only values too.
	encodedSubject := mime.QEncoding.Encode("utf-8", cleanSubject)
	encodedFromName := mime.QEncoding.Encode("utf-8", cleanFromName)

	fromHeader := cleanFrom
	if encodedFromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", encodedFromName, cleanFrom)
	}

	msg := "From: " + fromHeader + "\r\n" +
		"To: " + cleanTo + "\r\n" +
		"Subject: " + encodedSubject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=\"UTF-8\"\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"\r\n" +
		htmlBody

	return builtMessage{From: cleanFrom, To: cleanTo, Data: []byte(msg)}, nil
}

func sendWith(cfg config.SMTPConfig, to, subject, htmlBody string) error {
	addr := net.JoinHostPort(cfg.Host, cfg.Port)

	built, err := buildMessage(cfg, to, subject, htmlBody)
	if err != nil {
		return err
	}
	cleanFrom, cleanTo, msg := built.From, built.To, built.Data

	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)

	if cfg.TLS {
		err = sendWithTLS(addr, auth, cfg, cleanFrom, cleanTo, msg)
	} else {
		err = smtp.SendMail(addr, auth, cleanFrom, []string{cleanTo}, msg)
	}
	if err != nil {
		log.Printf("[smtp] send to %s failed: %v", cleanTo, err)
		return err
	}
	log.Printf("[smtp] sent to %s: %s", cleanTo, subject)
	return nil
}

// buildTLSConfig assembles the tls.Config used for the STARTTLS handshake.
// By default this verifies the server certificate against the system's
// trust store, same as before. Two admin-controlled opt-ins exist for mail
// servers whose certificate the system doesn't already trust (common for
// on-prem/internal mail relays using a private CA):
//
//   - cfg.CACertPEM: the extra CA certificate is trusted IN ADDITION TO the
//     system pool — the recommended fix, since the connection is still
//     verified, just against a trust list that now includes the internal CA.
//   - cfg.InsecureSkipVerify: disables certificate verification entirely.
//     This is a deliberate, explicit last resort (surfaced as its own toggle
//     in the mail settings UI, off by default) for when the CA certificate
//     isn't available — it removes protection against a man-in-the-middle
//     on the path to the mail server, so CACertPEM should always be tried
//     first.
func buildTLSConfig(cfg config.SMTPConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{ServerName: cfg.Host}

	if cfg.CACertPEM != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if ok := pool.AppendCertsFromPEM([]byte(cfg.CACertPEM)); !ok {
			return nil, errors.New("SMTP CA certificate is not valid PEM")
		}
		tlsConfig.RootCAs = pool
	}

	if cfg.InsecureSkipVerify {
		log.Printf("[smtp] WARNING: TLS certificate verification is disabled for %s (smtp_insecure_skip_verify=true) — this is insecure; configure the internal CA certificate instead if at all possible", cfg.Host)
		tlsConfig.InsecureSkipVerify = true //nolint:gosec // explicit, admin-controlled opt-in; see doc comment above
	}

	return tlsConfig, nil
}

// sendWithTLS speaks STARTTLS: connect in plaintext (as the SMTP submission
// port, almost always 587, expects) and then upgrade the connection before
// authenticating. This is NOT implicit/direct TLS (that's port 465, where the
// TLS handshake is the very first bytes on the wire) — dialing straight into
// TLS on a STARTTLS-only port fails immediately with "first record does not
// look like a TLS handshake" because the server's plaintext greeting isn't a
// valid TLS record.
func sendWithTLS(addr string, auth smtp.Auth, cfg config.SMTPConfig, from, to string, msg []byte) error {
	host := cfg.Host
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	tlsConfig, err := buildTLSConfig(cfg)
	if err != nil {
		return fmt.Errorf("starttls: %w", err)
	}
	if err := client.StartTLS(tlsConfig); err != nil {
		return fmt.Errorf("starttls: %w", err)
	}

	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close: %w", err)
	}
	return client.Quit()
}
