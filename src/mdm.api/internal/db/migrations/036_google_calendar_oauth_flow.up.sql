-- OAuth authorisation flow for the Google Calendar connection (see 035).
--
-- redirect_uris: the callback URLs registered for the client in Google Cloud
-- Console, copied from the uploaded JSON. A connect request may only use one
-- of them.
-- connected_account: the authorised Google account's address, so the settings
-- page can say which account is connected without calling Google each time.
ALTER TABLE google_calendar_settings ADD COLUMN IF NOT EXISTS redirect_uris     TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE google_calendar_settings ADD COLUMN IF NOT EXISTS connected_account TEXT   NOT NULL DEFAULT '';
