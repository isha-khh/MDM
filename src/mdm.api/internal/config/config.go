package config

import (
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	ListenAddr   string
	DatabaseURL  string
	JWTSecret    string
	MicroMDMURL  string
	MicroMDMKey  string
	VPPTokenPath string
	WebhookPath  string
	WebSocketURL string
	SMTP         SMTPConfig

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
		MicroMDMURL:  envOr("MICROMDM_URL", ""),
		MicroMDMKey:  envOr("MICROMDM_API_KEY", ""),
		VPPTokenPath: envOr("VPP_TOKEN_PATH", ""),
		WebhookPath:  envOr("WEBHOOK_PATH", "/webhook"),
		WebSocketURL: envOr("WEBSOCKET_URL", ""),
		SMTP: SMTPConfig{
			Host:     envOr("SMTP_HOST", ""),
			Port:     envOr("SMTP_PORT", "587"),
			Username: envOr("SMTP_USERNAME", ""),
			Password: envOr("SMTP_PASSWORD", ""),
			From:     envOr("SMTP_FROM", ""),
			FromName: envOr("SMTP_FROM_NAME", "MDM 管理平台"),
			TLS:      envOr("SMTP_TLS", "true") == "true",
		},

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
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
