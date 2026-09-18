-- Support trusting an internal/self-signed mail server CA certificate, and
-- (as an explicit, admin-controlled last resort) skipping TLS certificate
-- verification entirely for STARTTLS. Needed when the SMTP host presents a
-- certificate signed by an internal CA that isn't in the system trust store
-- (common for on-prem mail relays), which otherwise fails with:
--   starttls: tls: failed to verify certificate: x509: certificate signed by unknown authority
ALTER TABLE mail_settings ADD COLUMN IF NOT EXISTS smtp_ca_cert TEXT NOT NULL DEFAULT '';
ALTER TABLE mail_settings ADD COLUMN IF NOT EXISTS smtp_insecure_skip_verify BOOLEAN NOT NULL DEFAULT FALSE;
