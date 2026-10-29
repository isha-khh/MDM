package config

import (
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	ListenAddr  string
	DatabaseURL string
	JWTSecret   string
	// CookieSecure sets the Secure flag on the auth cookie. Defaults to true
	// (fail-safe) — local HTTP development must explicitly opt out via
	// COOKIE_SECURE=false, since browsers refuse to send a Secure cookie over
	// plain HTTP and a wrong default in the other direction would silently
	// ship an unprotected session cookie to production.
	CookieSecure bool
	MicroMDMURL  string
	MicroMDMKey  string
	VPPTokenPath string
	WebhookPath  string
	WebSocketURL string
	SMTP         SMTPConfig

	// OIDC / SSO
	OIDCEnabled      bool   // master switch; set OIDC_CLIENT_ID to enable
	OIDCIssuerURL    string // e.g. https://accounts.google.com
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string // e.g. https://mdm.example.com/api/auth/sso/callback

	// Apple Business Manager API + DEP auto-assignment
	ABMKeyPath      string        // PEM path; empty disables ABM
	ABMClientID     string        // BUSINESSAPI.<UUID>
	ABMTeamID       string        // defaults to ABMClientID
	ABMKeyID        string        // KID
	ABMScope        string        // defaults to "business.api"
	DEPTemplateDir  string        // dir holding mac.json/ipad.json/iphone.json
	DEPPollInterval time.Duration // how often to poll ABM
	DEPAutoAssign   bool          // master switch; must be true to start scheduler
}

type SMTPConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
	FromName string
	TLS      bool

	// CACertPEM, when non-empty, is trusted IN ADDITION TO the system's
	// default CA pool for the STARTTLS handshake — needed when the mail
	// server's certificate is signed by an internal/private CA that isn't
	// publicly trusted. This is the recommended fix for:
	//   starttls: tls: failed to verify certificate: x509: certificate signed by unknown authority
	CACertPEM string
	// InsecureSkipVerify disables TLS certificate verification entirely.
	// This is an explicit, admin-controlled last resort for when the
	// internal CA certificate isn't available — it removes protection
	// against a man-in-the-middle on the path to the mail server. Prefer
	// CACertPEM whenever possible.
	InsecureSkipVerify bool
}

func Load() *Config {
	// Load .env file if it exists (won't override existing env vars)
	if err := godotenv.Load(); err != nil {
		log.Println("config: no .env file found, using environment variables")
	}

	pollIntervalRaw := envOr("DEP_POLL_INTERVAL", "5m")
	pollInterval, err := time.ParseDuration(pollIntervalRaw)
	if err != nil {
		log.Printf("config: DEP_POLL_INTERVAL=%q invalid (%v), defaulting to 5m", pollIntervalRaw, err)
		pollInterval = 5 * time.Minute
	}

	return &Config{
		ListenAddr:   envOr("LISTEN_ADDR", ":8080"),
		DatabaseURL:  envOr("DATABASE_URL", "postgres://mdm:mdm@localhost:5432/mdm?sslmode=disable"),
		JWTSecret:    envOr("JWT_SECRET", "change-me-in-production"),
		CookieSecure: envOr("COOKIE_SECURE", "true") == "true",
		MicroMDMURL:  envOr("MICROMDM_URL", ""),
		MicroMDMKey:  envOr("MICROMDM_API_KEY", ""),
		VPPTokenPath: envOr("VPP_TOKEN_PATH", ""),
		WebhookPath:  envOr("WEBHOOK_PATH", "/webhook"),
		WebSocketURL: envOr("WEBSOCKET_URL", ""),
		SMTP: SMTPConfig{
			Host:               envOr("SMTP_HOST", ""),
			Port:               envOr("SMTP_PORT", "587"),
			Username:           envOr("SMTP_USERNAME", ""),
			Password:           envOr("SMTP_PASSWORD", ""),
			From:               envOr("SMTP_FROM", ""),
			FromName:           envOr("SMTP_FROM_NAME", "MDM 管理平台"),
			TLS:                envOr("SMTP_TLS", "true") == "true",
			CACertPEM:          readFileOr(envOr("SMTP_CA_CERT_PATH", ""), ""),
			InsecureSkipVerify: envOr("SMTP_INSECURE_SKIP_VERIFY", "false") == "true",
		},

		OIDCIssuerURL:    envOr("OIDC_ISSUER_URL", ""),
		OIDCClientID:     envOr("OIDC_CLIENT_ID", ""),
		OIDCClientSecret: envOr("OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:  envOr("OIDC_REDIRECT_URL", ""),
		OIDCEnabled:      envOr("OIDC_CLIENT_ID", "") != "",

		ABMKeyPath:      envOr("ABM_KEY_PATH", ""),
		ABMClientID:     envOr("ABM_CLIENT_ID", ""),
		ABMTeamID:       envOr("ABM_TEAM_ID", ""),
		ABMKeyID:        envOr("ABM_KEY_ID", ""),
		ABMScope:        envOr("ABM_SCOPE", "business.api"),
		DEPTemplateDir:  envOr("DEP_TEMPLATE_DIR", "./dep-profiles"),
		DEPPollInterval: pollInterval,
		DEPAutoAssign:   envOr("DEP_AUTO_ASSIGN", "false") == "true",
	}
}

func envOr(key, fallback string) string {
	v := os.Getenv(key)
	// Defensive: some docker-compose versions don't strip inline comments
	// from .env, so "DEP_AUTO_ASSIGN=true   # ...comment..." can arrive as
	// the full string. Cut at " #" then trim whitespace.
	if i := strings.Index(v, " #"); i >= 0 {
		v = v[:i]
	}
	v = strings.TrimSpace(v)
	if v != "" {
		return v
	}
	return fallback
}

// readFileOr reads path's content (e.g. a PEM-encoded CA certificate) if
// path is non-empty, returning fallback if path is empty or the file can't
// be read. Mirrors how ABMKeyPath/VPPTokenPath are handled elsewhere: env
// vars point at a file path rather than embedding file content inline.
func readFileOr(path, fallback string) string {
	if path == "" {
		return fallback
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("config: failed to read %q: %v", path, err)
		return fallback
	}
	return string(data)
}
