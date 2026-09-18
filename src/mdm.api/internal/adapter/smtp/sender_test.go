package smtp

import (
	"strings"
	"testing"

	"github.com/anthropics/mdm-server/internal/config"
)

func TestBuildMessage_EncodesChineseSubjectAndFromName(t *testing.T) {
	cfg := config.SMTPConfig{
		From:     "noreply@example.com",
		FromName: "資產管理系統",
	}

	built, err := buildMessage(cfg, "user@example.com", "設備維修申請已核准", "<p>內容</p>")
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}
	msg := string(built.Data)

	// The raw UTF-8 bytes must NOT appear directly in a header line — that's
	// exactly the mojibake bug being fixed.
	if strings.Contains(headerSection(msg), "設備維修申請已核准") {
		t.Error("subject header contains raw UTF-8 bytes; should be RFC 2047 encoded")
	}
	if strings.Contains(headerSection(msg), "資產管理系統") {
		t.Error("From display name contains raw UTF-8 bytes; should be RFC 2047 encoded")
	}

	if !strings.Contains(msg, "Subject: =?utf-8?q?") {
		t.Errorf("expected RFC 2047 encoded-word Subject header, got message:\n%s", msg)
	}
	if !strings.Contains(msg, "From: =?utf-8?q?") {
		t.Errorf("expected RFC 2047 encoded-word From header, got message:\n%s", msg)
	}

	// The body is the one place raw UTF-8 is fine — it's declared UTF-8/8bit
	// via Content-Type/Content-Transfer-Encoding, not subject to header ASCII
	// rules.
	if !strings.Contains(msg, "<p>內容</p>") {
		t.Error("body should contain the raw HTML unchanged")
	}
	if !strings.Contains(msg, "Content-Transfer-Encoding: 8bit") {
		t.Error("expected Content-Transfer-Encoding: 8bit header for the UTF-8 body")
	}
}

func TestBuildMessage_ASCIIOnlyPassesThroughUnencoded(t *testing.T) {
	cfg := config.SMTPConfig{From: "noreply@example.com", FromName: "MDM Console"}

	built, err := buildMessage(cfg, "user@example.com", "Rental Approved", "<p>Hello</p>")
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}
	msg := string(built.Data)

	// Pure-ASCII values should NOT get wrapped in encoded-words — that would
	// be needless overhead and a behavior change for the common case.
	if strings.Contains(msg, "=?utf-8?") {
		t.Errorf("ASCII-only subject/from-name should not be RFC 2047 encoded, got:\n%s", msg)
	}
	if !strings.Contains(msg, "Subject: Rental Approved") {
		t.Errorf("expected plain Subject header, got:\n%s", msg)
	}
}

func TestBuildMessage_RejectsHeaderInjectionInSubject(t *testing.T) {
	cfg := config.SMTPConfig{From: "noreply@example.com"}

	// A CRLF in the subject is a header-injection attempt (e.g. smuggling a
	// Bcc: line). buildMessage must refuse to send rather than silently
	// stripping and sending anyway — a value that needed cleaning means the
	// input didn't come from where the caller expected.
	if _, err := buildMessage(cfg, "user@example.com", "Subject\r\nBcc: attacker@evil.com", "body"); err == nil {
		t.Error("expected buildMessage to reject a subject containing CRLF, not silently clean it")
	}
}

func TestBuildMessage_RejectsHeaderInjectionInFromName(t *testing.T) {
	cfg := config.SMTPConfig{From: "noreply@example.com", FromName: "MDM\r\nBcc: attacker@evil.com"}

	if _, err := buildMessage(cfg, "user@example.com", "Subject", "body"); err == nil {
		t.Error("expected buildMessage to reject a From display name containing CRLF")
	}
}

func TestBuildMessage_RejectsInvalidEmailAddresses(t *testing.T) {
	cases := []struct {
		name string
		to   string
		from string
	}{
		{"empty to", "", "noreply@example.com"},
		{"empty from", "user@example.com", ""},
		{"malformed to", "not-an-email", "noreply@example.com"},
		{"malformed from", "user@example.com", "not-an-email"},
		{"CRLF in to", "user@example.com\r\nBcc: attacker@evil.com", "noreply@example.com"},
		{"CRLF in from", "user@example.com", "noreply@example.com\r\nBcc: attacker@evil.com"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := buildMessage(config.SMTPConfig{From: c.from}, c.to, "s", "b"); err == nil {
				t.Errorf("expected buildMessage to reject %s (to=%q, from=%q)", c.name, c.to, c.from)
			}
		})
	}
}

func TestBuildMessage_AcceptsAddressWithDisplayNameAndUsesBareAddress(t *testing.T) {
	// net/mail.ParseAddress accepts "Name <addr>" form; buildMessage should
	// extract just the bare address for the To:/From: envelope rather than
	// carrying the caller-supplied display name through unchanged (that
	// display-name portion is itself an injection vector).
	cfg := config.SMTPConfig{From: "Ops Team <noreply@example.com>"}

	built, err := buildMessage(cfg, "A User <user@example.com>", "s", "b")
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}
	if built.To != "user@example.com" {
		t.Errorf("expected bare To address, got %q", built.To)
	}
	if built.From != "noreply@example.com" {
		t.Errorf("expected bare From address, got %q", built.From)
	}
}

// headerSection returns everything before the blank-line separator, i.e. the
// raw header block, so body content isn't accidentally matched by header
// assertions above.
func headerSection(msg string) string {
	if i := strings.Index(msg, "\r\n\r\n"); i >= 0 {
		return msg[:i]
	}
	return msg
}

func TestBuildTLSConfig_DefaultUsesSystemTrustStore(t *testing.T) {
	cfg := config.SMTPConfig{Host: "mail.example.com"}

	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if tlsCfg.ServerName != "mail.example.com" {
		t.Errorf("expected ServerName=mail.example.com, got %q", tlsCfg.ServerName)
	}
	if tlsCfg.RootCAs != nil {
		t.Error("expected RootCAs to be nil (system default pool) when no CACertPEM is configured")
	}
	if tlsCfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify must default to false")
	}
}

// A real (throwaway) self-signed cert, generated once for this test via:
//
//	openssl req -x509 -newkey rsa:2048 -nodes -keyout /dev/null \
//	  -days 36500 -subj "/CN=test-ca" -out ca.pem
const testCACertPEM = `-----BEGIN CERTIFICATE-----
MIICojCCAYoCCQCPUfqe8L+MXjANBgkqhkiG9w0BAQsFADASMRAwDgYDVQQDDAd0
ZXN0LWNhMCAXDTI2MDkxODA4MzU1NVoYDzIxMjYwODI1MDgzNTU1WjASMRAwDgYD
VQQDDAd0ZXN0LWNhMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAz+GS
RkB/vsn+H/H/UpqenXuErEl4342n3afZR1H7Zosvk5N070IRTMYNEAnqnOvbaxE+
GilZu+20AJ+f0E2JyPgl8bewiy7ySZtNykrT3PFIyMlSjdLfVg6FutESP+xsOXuI
7BemCh/9Bq3R6luK7TpSP6i5LBwW5T7gfgehUUIFHKuCQZP1n5g/zs+Q0g0DP36D
8uTrNGM08Oaw1WM2ywMJq9mtWhW5cO06YbXsP8YsqKPMQ+AXOa2YqKTk1bawdqKM
gJtyCxlkCkHOxpXc1J492Gr8J8RAUfJUwziC9slXq9SfCE1dsrQkJ88nNhAKhjrR
fFOwSAfL43jn9+R/dwIDAQABMA0GCSqGSIb3DQEBCwUAA4IBAQCKhIKlAccR2LiT
RF3si3+750TKMBK/oApGWT2+hwTalsS8KhdISTr4mdKd9UvbJngOijeX2fUWgJzJ
qJfCckpu+M0cY9JBtwSsUkKltF1JgJTWyZEYE6PphH1ebWfusB74+3H9LkbJl7Np
NxCcSNA3tc5Qm5DbOYFbWxEnqXqdvyfwAjY/VlEcCEotMZrsFJySE/r0ILUoz6vy
4cmGxh/Cg2PQloBYts1amr20sUK4n52Ncb/X3xUhYcCMHUDr+l1aP6rpOvBTSOii
VP1hRnXnc1GiK/h6Nc9kCIbyl7hpzO6b4xpasJkqrKg6R/Pgun/EHJZg+WjdG554
bwgyWX4P
-----END CERTIFICATE-----`

func TestBuildTLSConfig_TrustsConfiguredCACert(t *testing.T) {
	cfg := config.SMTPConfig{Host: "mail.example.com", CACertPEM: testCACertPEM}

	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if tlsCfg.RootCAs == nil {
		t.Fatal("expected RootCAs to be populated when CACertPEM is configured")
	}
	// Subjects() is deprecated but is the simplest way to assert the pool
	// actually contains our certificate's issuer, not just "a" pool.
	found := false
	for _, s := range tlsCfg.RootCAs.Subjects() { //nolint:staticcheck // test-only introspection
		if strings.Contains(string(s), "test-ca") {
			found = true
		}
	}
	if !found {
		t.Error("expected the configured CA certificate to be present in the trust pool")
	}
}

func TestBuildTLSConfig_RejectsInvalidPEM(t *testing.T) {
	cfg := config.SMTPConfig{Host: "mail.example.com", CACertPEM: "not a certificate"}

	if _, err := buildTLSConfig(cfg); err == nil {
		t.Error("expected buildTLSConfig to reject malformed PEM content")
	}
}

func TestBuildTLSConfig_InsecureSkipVerifyIsOptIn(t *testing.T) {
	cfg := config.SMTPConfig{Host: "mail.example.com", InsecureSkipVerify: true}

	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if !tlsCfg.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be honored when explicitly set")
	}
}
