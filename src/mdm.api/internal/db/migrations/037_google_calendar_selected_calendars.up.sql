-- Which of the connected account's calendars to read business trips from.
-- Belongs to a connection: it is reset when the credentials are replaced, the
-- account is disconnected, or a different Google account is connected.
ALTER TABLE google_calendar_settings ADD COLUMN IF NOT EXISTS selected_calendar_ids TEXT[] NOT NULL DEFAULT '{}';
