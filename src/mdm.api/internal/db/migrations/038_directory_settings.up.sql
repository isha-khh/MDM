-- Connection to the company user directory API (GET {base_url}/api/users with
-- an X-API-Key header), used to look up whether a business-trip attendee has a
-- driver's license and a car of their own. Single row, like mail_settings.
-- api_key_enc is AES-256-GCM ciphertext (see internal/secretbox); the key
-- lives in SETTINGS_ENCRYPTION_KEY, not here.
CREATE TABLE IF NOT EXISTS directory_settings (
    id          TEXT        PRIMARY KEY DEFAULT 'default',
    base_url    TEXT        NOT NULL DEFAULT '',
    api_key_enc TEXT        NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  TEXT
);

INSERT INTO directory_settings (id) VALUES ('default') ON CONFLICT DO NOTHING;
