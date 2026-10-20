-- Google Calendar connection, used to read business-trip events from a
-- calendar the connected Google account is subscribed to (read-only).
-- Single-row table keyed by id='default', like mail_settings.
--
-- client_secret_enc / refresh_token_enc hold AES-256-GCM ciphertext
-- ("v1:..." from internal/secretbox); the key lives in the
-- SETTINGS_ENCRYPTION_KEY environment variable, not in this database. They
-- are empty strings until the credentials are uploaded / the account is
-- authorised. client_id is not a secret and is stored as is.
CREATE TABLE IF NOT EXISTS google_calendar_settings (
    id                TEXT        PRIMARY KEY DEFAULT 'default',
    client_id         TEXT        NOT NULL DEFAULT '',
    client_secret_enc TEXT        NOT NULL DEFAULT '',
    refresh_token_enc TEXT        NOT NULL DEFAULT '',
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by        TEXT
);

INSERT INTO google_calendar_settings (id) VALUES ('default') ON CONFLICT DO NOTHING;
