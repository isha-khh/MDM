-- Log of business-trip rental-car notice mails. One row per (calendar event,
-- event start, recipient): the UNIQUE key is what stops a person being mailed
-- twice about the same trip, even if two admins press "send" together. A
-- rescheduled trip has a new event_start, so it is announced again.
--
-- status: 'pending' (claimed, mail in flight), 'sent', 'failed' (may be retried).
CREATE TABLE IF NOT EXISTS trip_car_notices (
    id              BIGSERIAL   PRIMARY KEY,
    calendar_id     TEXT        NOT NULL,
    event_id        TEXT        NOT NULL,
    event_start     TEXT        NOT NULL,
    event_summary   TEXT        NOT NULL DEFAULT '',
    recipient_email TEXT        NOT NULL,
    recipient_account TEXT      NOT NULL DEFAULT '',
    status          TEXT        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    error           TEXT        NOT NULL DEFAULT '',
    created_by      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at         TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (calendar_id, event_id, event_start, recipient_email)
);

CREATE INDEX IF NOT EXISTS idx_trip_car_notices_created ON trip_car_notices (created_at DESC);
